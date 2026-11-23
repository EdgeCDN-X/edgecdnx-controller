package caching

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const apisixTLSManagedLabel = "edgecdnx.com/apisix-tls"

type apisixTLSMaterial struct {
	cert []byte
	key  []byte
}

// +kubebuilder:rbac:groups=apisix.apache.org,resources=apisixtlses;apisixroutes;apisixupstreams,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *ApacheApisixCacheReconciler) reconcileTLS(ctx context.Context, service *api.Service) error {
	secrets := make(map[string]apisixTLSMaterial)
	tlsResources := make(map[string]apisixv2.ApisixTls)
	hosts := make(map[string]struct{})

	add := func(hostname string, certificate api.CertificateSpec) error {
		if certificate.Crt == "" && certificate.Key == "" {
			return nil
		}
		if certificate.Crt == "" || certificate.Key == "" {
			return fmt.Errorf("certificate for %q must include both crt and key", hostname)
		}
		if problems := validation.IsDNS1123Subdomain(certificate.SecretRef); len(problems) != 0 {
			return fmt.Errorf("certificate for %q requires a valid secretRef: %s", hostname, strings.Join(problems, ", "))
		}
		host := strings.ToLower(strings.TrimSuffix(hostname, "."))
		if len(validation.IsDNS1123Subdomain(host)) != 0 && len(validation.IsWildcardDNS1123Subdomain(host)) != 0 {
			return fmt.Errorf("invalid TLS hostname %q", hostname)
		}
		if _, exists := hosts[host]; exists {
			return fmt.Errorf("duplicate TLS hostname %q", hostname)
		}
		hosts[host] = struct{}{}
		crt, err := base64.StdEncoding.DecodeString(certificate.Crt)
		if err != nil {
			return fmt.Errorf("decode certificate for %q: %w", hostname, err)
		}
		key, err := base64.StdEncoding.DecodeString(certificate.Key)
		if err != nil {
			return fmt.Errorf("decode private key for %q: %w", hostname, err)
		}
		pair, err := tls.X509KeyPair(crt, key)
		if err != nil {
			return fmt.Errorf("invalid TLS certificate or key for %q: %w", hostname, err)
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return fmt.Errorf("parse TLS certificate for %q: %w", hostname, err)
		}
		if err := leaf.VerifyHostname(host); err != nil {
			return fmt.Errorf("TLS certificate does not cover %q: %w", hostname, err)
		}
		material := apisixTLSMaterial{cert: crt, key: key}
		if existing, exists := secrets[certificate.SecretRef]; exists &&
			(!bytes.Equal(existing.cert, crt) || !bytes.Equal(existing.key, key)) {
			return fmt.Errorf("secretRef %q is used by different TLS certificates", certificate.SecretRef)
		}
		secrets[certificate.SecretRef] = material
		name := apisixTLSName(service.Name, host)
		tlsResources[name] = apisixv2.ApisixTls{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: service.Namespace, Labels: map[string]string{apisixTLSManagedLabel: "true"}},
			Spec: apisixv2.ApisixTlsSpec{
				IngressClassName: service.Spec.Cache,
				Hosts:            []apisixv2.HostType{apisixv2.HostType(host)},
				Secret:           apisixv2.ApisixSecret{Name: certificate.SecretRef, Namespace: service.Namespace},
			},
		}
		return nil
	}

	if err := add(service.Spec.Domain, service.Spec.Certificate); err != nil {
		return err
	}
	for _, alias := range service.Spec.HostAliases {
		if err := add(alias.Name, alias.Certificate); err != nil {
			return err
		}
	}

	// Refuse to overwrite resources not created by this controller.
	for name := range secrets {
		current := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: name}, current); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("get TLS secret %s/%s: %w", service.Namespace, name, err)
			}
		} else if !metav1.IsControlledBy(current, service) || current.Labels[apisixTLSManagedLabel] != "true" {
			return fmt.Errorf("TLS secret %s/%s is not managed by this Service", service.Namespace, name)
		}
	}
	for name := range tlsResources {
		current := &apisixv2.ApisixTls{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: name}, current); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("get ApisixTls %s/%s: %w", service.Namespace, name, err)
			}
		} else if !metav1.IsControlledBy(current, service) || current.Labels[apisixTLSManagedLabel] != "true" {
			return fmt.Errorf("ApisixTls %s/%s is not managed by this Service", service.Namespace, name)
		}
	}
	for name, material := range secrets {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: service.Namespace}}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
			if secret.ResourceVersion != "" && (!metav1.IsControlledBy(secret, service) || secret.Labels[apisixTLSManagedLabel] != "true") {
				return fmt.Errorf("TLS secret %s/%s is not managed by this Service", service.Namespace, name)
			}
			if err := controllerutil.SetControllerReference(service, secret, r.Scheme); err != nil {
				return err
			}
			if secret.Labels == nil {
				secret.Labels = make(map[string]string)
			}
			secret.Labels[apisixTLSManagedLabel] = "true"
			secret.Type = corev1.SecretTypeTLS
			if secret.Data == nil {
				secret.Data = make(map[string][]byte)
			}
			secret.Data[corev1.TLSCertKey] = material.cert
			secret.Data[corev1.TLSPrivateKeyKey] = material.key
			return nil
		})
		if err != nil {
			return fmt.Errorf("reconcile TLS secret %s/%s: %w", service.Namespace, name, err)
		}
	}
	for name, desired := range tlsResources {
		current := &apisixv2.ApisixTls{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: service.Namespace}}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
			if current.ResourceVersion != "" && (!metav1.IsControlledBy(current, service) || current.Labels[apisixTLSManagedLabel] != "true") {
				return fmt.Errorf("ApisixTls %s/%s is not managed by this Service", service.Namespace, name)
			}
			if err := controllerutil.SetControllerReference(service, current, r.Scheme); err != nil {
				return err
			}
			if current.Labels == nil {
				current.Labels = make(map[string]string)
			}
			current.Labels[apisixTLSManagedLabel] = "true"
			current.Spec = desired.Spec
			return nil
		})
		if err != nil {
			return fmt.Errorf("reconcile ApisixTls %s/%s: %w", service.Namespace, name, err)
		}
	}

	currentTLS := &apisixv2.ApisixTlsList{}
	if err := r.List(ctx, currentTLS, client.InNamespace(service.Namespace), client.MatchingLabels{apisixTLSManagedLabel: "true"}); err != nil {
		return fmt.Errorf("list ApisixTls resources: %w", err)
	}
	for i := range currentTLS.Items {
		current := &currentTLS.Items[i]
		if _, wanted := tlsResources[current.Name]; wanted || !metav1.IsControlledBy(current, service) {
			continue
		}
		if err := r.Delete(ctx, current); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete stale ApisixTls %s: %w", client.ObjectKeyFromObject(current), err)
		}
	}
	currentSecrets := &corev1.SecretList{}
	if err := r.List(ctx, currentSecrets, client.InNamespace(service.Namespace), client.MatchingLabels{apisixTLSManagedLabel: "true"}); err != nil {
		return fmt.Errorf("list TLS secrets: %w", err)
	}
	for i := range currentSecrets.Items {
		current := &currentSecrets.Items[i]
		if _, wanted := secrets[current.Name]; wanted || !metav1.IsControlledBy(current, service) {
			continue
		}
		if err := r.Delete(ctx, current); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete stale TLS secret %s: %w", client.ObjectKeyFromObject(current), err)
		}
	}
	return nil
}

func apisixTLSName(serviceName, hostname string) string {
	hash := sha256.Sum256([]byte(hostname))
	suffix := fmt.Sprintf("-tls-%x", hash[:8])
	if len(serviceName)+len(suffix) > validation.DNS1123SubdomainMaxLength {
		serviceName = strings.TrimRight(serviceName[:validation.DNS1123SubdomainMaxLength-len(suffix)], "-.")
	}
	return serviceName + suffix
}
