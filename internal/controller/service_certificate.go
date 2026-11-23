package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	certificateSecretIndex  = ".spec.secretName"
	serviceCertificateLabel = "edgecdnx.com/service-certificate"
	solverTargetLabel       = "solver-target"
)

// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services,verbs=get;list;watch;patch

func (r *ServiceReconciler) reconcileServiceCertificates(ctx context.Context, service *api.Service) error {
	issuer := r.ClusterIssuerName
	if issuer == "" {
		issuer = "edgecdnx"
	}
	original := service.DeepCopy()
	desiredNames := make(map[string]struct{})
	seenDNSNames := make(map[string]struct{})

	reconcileName := func(hostname, target string, destination *api.CertificateSpec) error {
		dnsName := strings.ToLower(strings.TrimSuffix(hostname, "."))
		if dnsName == "" {
			return fmt.Errorf("cannot issue a certificate for an empty hostname")
		}
		if _, exists := seenDNSNames[dnsName]; exists {
			return fmt.Errorf("duplicate service certificate hostname %q", hostname)
		}
		seenDNSNames[dnsName] = struct{}{}
		name := serviceDNSEndpointName(service.Name, dnsName, "cert")
		// A matching reference identifies a certificate previously managed by this controller.
		managed := destination.CertificateRef == name && destination.SecretRef == name
		if !managed && *destination != (api.CertificateSpec{}) {
			return nil
		}
		desiredNames[name] = struct{}{}
		certBuilder, err := builder.CertBuilderFactory(builder.Service, name, service.Namespace, service.Name,
			cmmeta.ObjectReference{Name: issuer, Kind: "ClusterIssuer", Group: certmanagerv1.SchemeGroupVersion.Group})
		if err != nil {
			return err
		}
		certBuilder.WithDNSNames([]string{dnsName})
		certBuilder.WithSecretName(name)
		desired, _, err := certBuilder.Build()
		if err != nil {
			return err
		}
		desired.Labels = map[string]string{serviceCertificateLabel: "true", solverTargetLabel: target}
		if err := controllerutil.SetControllerReference(service, &desired, r.Scheme); err != nil {
			return err
		}

		current := &certmanagerv1.Certificate{}
		key := client.ObjectKeyFromObject(&desired)
		if err := r.Get(ctx, key, current); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			return r.Create(ctx, &desired)
		}
		if !metav1.IsControlledBy(current, service) || current.Labels[serviceCertificateLabel] != "true" {
			return fmt.Errorf("certificate %s is not managed by service %s", key, client.ObjectKeyFromObject(service))
		}
		before := current.DeepCopy()
		current.Spec.DNSNames = desired.Spec.DNSNames
		current.Spec.IssuerRef = desired.Spec.IssuerRef
		current.Spec.SecretName = desired.Spec.SecretName
		current.Spec.RenewBefore = desired.Spec.RenewBefore
		if current.Labels == nil {
			current.Labels = make(map[string]string)
		}
		current.Labels[solverTargetLabel] = target
		if !reflect.DeepEqual(before.Spec, current.Spec) || !reflect.DeepEqual(before.Labels, current.Labels) {
			if err := r.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
			// Wait for cert-manager to observe any changes before consuming the secret.
			if !reflect.DeepEqual(before.Spec, current.Spec) {
				return nil
			}
		}
		ready := false
		for _, condition := range current.Status.Conditions {
			if condition.Type == certmanagerv1.CertificateConditionReady && condition.Status == cmmeta.ConditionTrue &&
				condition.ObservedGeneration == current.Generation {
				ready = true
				break
			}
		}
		if !ready {
			return nil
		}
		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: current.Spec.SecretName}, secret); err != nil {
			return fmt.Errorf("get secret for ready certificate %s: %w", key, err)
		}
		crt, keyData := secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey]
		if len(crt) == 0 || len(keyData) == 0 {
			return fmt.Errorf("secret for ready certificate %s is missing tls.crt or tls.key", key)
		}
		*destination = api.CertificateSpec{
			CertificateRef: current.Name,
			SecretRef:      current.Spec.SecretName,
			Crt:            base64.StdEncoding.EncodeToString(crt),
			Key:            base64.StdEncoding.EncodeToString(keyData),
		}
		return nil
	}

	if err := reconcileName(service.Spec.Domain, "delegation", &service.Spec.Certificate); err != nil {
		return err
	}
	for i := range service.Spec.HostAliases {
		alias := &service.Spec.HostAliases[i]
		target := "delegation"
		switch alias.TargetType {
		case "", "DELEGATION":
		case "CNAME":
			target = "cname"
		default:
			return fmt.Errorf("unsupported host alias targetType %q", alias.TargetType)
		}
		if err := reconcileName(alias.Name, target, &alias.Certificate); err != nil {
			return err
		}
	}
	if !reflect.DeepEqual(original.Spec, service.Spec) {
		if err := r.Patch(ctx, service, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	certificates := &certmanagerv1.CertificateList{}
	if err := r.List(ctx, certificates, client.InNamespace(service.Namespace), client.MatchingLabels{serviceCertificateLabel: "true"}); err != nil {
		return err
	}
	for i := range certificates.Items {
		certificate := &certificates.Items[i]
		if !metav1.IsControlledBy(certificate, service) {
			continue
		}
		if _, desired := desiredNames[certificate.Name]; desired {
			continue
		}
		if err := r.Delete(ctx, certificate); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func (r *ServiceReconciler) servicesForCertificateSecret(ctx context.Context, object client.Object) []reconcile.Request {
	certificates := &certmanagerv1.CertificateList{}
	if err := r.List(ctx, certificates, client.InNamespace(object.GetNamespace()),
		client.MatchingFields{certificateSecretIndex: object.GetName()},
		client.MatchingLabels{serviceCertificateLabel: "true"}); err != nil {
		logf.FromContext(ctx).Error(err, "Unable to list certificates for secret change", "secret", client.ObjectKeyFromObject(object))
		return nil
	}
	requests := make([]reconcile.Request, 0, len(certificates.Items))
	for i := range certificates.Items {
		owner := metav1.GetControllerOf(&certificates.Items[i])
		if owner != nil && owner.APIVersion == api.SchemeGroupVersion.String() && owner.Kind == "Service" {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{Namespace: object.GetNamespace(), Name: owner.Name}})
		}
	}
	return requests
}
