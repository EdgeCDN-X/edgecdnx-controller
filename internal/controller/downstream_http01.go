package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	http01ChallengeKind     = "Challenge"
	http01ChallengeName     = "downstream.edgecdnx.com/challenge-name"
	http01ChallengeID       = "downstream.edgecdnx.com/challenge-id"
	http01SolverPort        = 8089
	http01ReconcileInterval = 30 * time.Second
)

var http01TokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// +kubebuilder:rbac:groups=acme.cert-manager.io,resources=challenges;orders,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificaterequests,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apisix.apache.org,resources=apisixroutes,verbs=get;list;watch;create;update;patch;delete

func http01ResourceName(key types.NamespacedName) string {
	hash := sha256.Sum256([]byte(key.Namespace + "/" + key.Name))
	return fmt.Sprintf("edgecdnx-acme-%x", hash[:12])
}

func (r *DownstreamReconciler) reconcileHTTP01Challenge(ctx context.Context, key types.NamespacedName) error {
	challenge := &acmev1.Challenge{}
	if err := r.UpstreamReader.Get(ctx, key, challenge); err != nil {
		if apierrors.IsNotFound(err) {
			return r.deleteHTTP01Resources(ctx, key)
		}
		return fmt.Errorf("get upstream challenge %s: %w", key, err)
	}
	if !challenge.DeletionTimestamp.IsZero() || challenge.Spec.Type != acmev1.ACMEChallengeTypeHTTP01 ||
		!challenge.Status.Processing || challenge.Status.State == acmev1.Valid ||
		challenge.Status.State == acmev1.Invalid || challenge.Status.State == acmev1.Expired || challenge.Status.State == acmev1.Errored {
		return r.deleteHTTP01Resources(ctx, key)
	}
	if r.queue != nil {
		defer r.queue.AddAfter(downstreamQueueKey{Kind: http01ChallengeKind, Key: key}, http01ReconcileInterval)
	}

	service, err := r.http01Service(ctx, challenge)
	if err != nil {
		return err
	}
	if service == nil {
		return r.deleteHTTP01Resources(ctx, key)
	}
	host := strings.ToLower(strings.TrimSuffix(challenge.Spec.DNSName, "."))
	if len(validation.IsDNS1123Subdomain(host)) != 0 || !http01TokenPattern.MatchString(challenge.Spec.Token) ||
		!strings.HasPrefix(challenge.Spec.Key, challenge.Spec.Token+".") ||
		!http01TokenPattern.MatchString(strings.TrimPrefix(challenge.Spec.Key, challenge.Spec.Token+".")) {
		return errors.Join(fmt.Errorf("challenge %s has invalid HTTP-01 host, token, or key authorization", key),
			r.deleteHTTP01Resources(ctx, key))
	}
	if service.Spec.Cache == "" {
		return errors.Join(fmt.Errorf("service %s has no APISIX cache class", client.ObjectKeyFromObject(service)),
			r.deleteHTTP01Resources(ctx, key))
	}
	if err := r.ensureNamespace(ctx, key.Namespace); err != nil {
		return err
	}
	name := http01ResourceName(key)
	labels := map[string]string{
		DownstreamManagedLabel: r.SourceID,
		http01ChallengeID:      name,
	}
	annotations := map[string]string{http01ChallengeName: key.Name, "sidecar.istio.io/inject": "false"}
	if err := r.ensureHTTP01Pod(ctx, key.Namespace, name, labels, annotations, host, challenge); err != nil {
		return err
	}
	if err := r.ensureHTTP01Service(ctx, key.Namespace, name, labels, annotations); err != nil {
		return err
	}
	if err := r.ensureHTTP01Route(ctx, key.Namespace, name, labels, annotations, host, challenge.Spec.Token, service.Spec.Cache); err != nil {
		return err
	}
	return nil
}

func (r *DownstreamReconciler) http01Service(ctx context.Context, challenge *acmev1.Challenge) (*api.Service, error) {
	order := &acmev1.Order{}
	request := &certv1.CertificateRequest{}
	certificate := &certv1.Certificate{}
	service := &api.Service{}
	for _, step := range []struct {
		child      client.Object
		kind       string
		apiVersion string
		parent     client.Object
	}{
		{challenge, "Order", acmev1.SchemeGroupVersion.String(), order},
		{order, "CertificateRequest", certv1.SchemeGroupVersion.String(), request},
		{request, "Certificate", certv1.SchemeGroupVersion.String(), certificate},
		{certificate, "Service", api.SchemeGroupVersion.String(), service},
	} {
		owner := metav1.GetControllerOf(step.child)
		if owner == nil || owner.Kind != step.kind || owner.APIVersion != step.apiVersion || owner.UID == "" {
			return nil, nil
		}
		if err := r.UpstreamReader.Get(ctx, types.NamespacedName{Namespace: challenge.Namespace, Name: owner.Name}, step.parent); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("get upstream %s %s/%s: %w", step.kind, challenge.Namespace, owner.Name, err)
		}
		if step.parent.GetUID() != owner.UID || !step.parent.GetDeletionTimestamp().IsZero() {
			return nil, nil
		}
	}
	if certificate.Labels[serviceCertificateLabel] != "true" || certificate.Labels[solverTargetLabel] != "cname" {
		return nil, nil
	}
	host := strings.ToLower(strings.TrimSuffix(challenge.Spec.DNSName, "."))
	foundCertificateName := false
	for _, dnsName := range certificate.Spec.DNSNames {
		if strings.ToLower(strings.TrimSuffix(dnsName, ".")) == host {
			foundCertificateName = true
			break
		}
	}
	if !foundCertificateName {
		return nil, nil
	}
	for _, alias := range service.Spec.HostAliases {
		if alias.TargetType == "CNAME" && strings.ToLower(strings.TrimSuffix(alias.Name, ".")) == host {
			return service, nil
		}
	}
	return nil, nil
}

func (r *DownstreamReconciler) ensureHTTP01Pod(ctx context.Context, namespace, name string, labels, annotations map[string]string, host string, challenge *acmev1.Challenge) error {
	desired := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels, Annotations: annotations},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: boolPointer(false),
			EnableServiceLinks:           boolPointer(false),
			RestartPolicy:                corev1.RestartPolicyOnFailure,
			NodeSelector:                 map[string]string{"kubernetes.io/os": "linux"},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   boolPointer(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:  "acmesolver",
				Image: r.HTTP01SolverImage,
				Args: []string{
					fmt.Sprintf("--listen-port=%d", http01SolverPort),
					"--domain=" + host,
					"--token=" + challenge.Spec.Token,
					"--key=" + challenge.Spec.Key,
				},
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: http01SolverPort}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
				ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(http01SolverPort)},
				}},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPointer(false),
					ReadOnlyRootFilesystem:   boolPointer(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
	current := &corev1.Pod{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Local.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Local.Create(ctx, desired)
		}
		return err
	}
	if err := r.checkHTTP01Ownership(current, labels, annotations); err != nil {
		return err
	}
	if !current.DeletionTimestamp.IsZero() {
		return fmt.Errorf("HTTP-01 solver pod %s is terminating", key)
	}
	matches := false
	for _, container := range current.Spec.Containers {
		if container.Name == "acmesolver" && container.Image == r.HTTP01SolverImage &&
			reflect.DeepEqual(container.Args, desired.Spec.Containers[0].Args) {
			matches = true
			break
		}
	}
	if !matches {
		if err := r.Local.Delete(ctx, current); client.IgnoreNotFound(err) != nil {
			return err
		}
		return fmt.Errorf("recreating changed HTTP-01 solver pod %s", key)
	}
	return nil
}

func boolPointer(value bool) *bool { return &value }

func (r *DownstreamReconciler) ensureHTTP01Service(ctx context.Context, namespace, name string, labels, annotations map[string]string) error {
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels, Annotations: annotations},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: http01SolverPort, TargetPort: intstr.FromInt(http01SolverPort)}},
		},
	}
	current := &corev1.Service{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Local.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Local.Create(ctx, desired)
		}
		return err
	}
	if err := r.checkHTTP01Ownership(current, labels, annotations); err != nil {
		return err
	}
	if reflect.DeepEqual(current.Spec.Selector, desired.Spec.Selector) && reflect.DeepEqual(current.Spec.Ports, desired.Spec.Ports) {
		return nil
	}
	current.Spec.Selector = desired.Spec.Selector
	current.Spec.Ports = desired.Spec.Ports
	return r.Local.Update(ctx, current)
}

func (r *DownstreamReconciler) ensureHTTP01Route(ctx context.Context, namespace, name string, labels, annotations map[string]string, host, token, cacheClass string) error {
	desired := &apisixv2.ApisixRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels, Annotations: annotations},
		Spec: apisixv2.ApisixRouteSpec{
			IngressClassName: cacheClass,
			HTTP: []apisixv2.ApisixRouteHTTP{{
				Name: "http01", Priority: 10000,
				Match: apisixv2.ApisixRouteHTTPMatch{
					Hosts: []string{host}, Paths: []string{"/.well-known/acme-challenge/" + token}, Methods: []string{"GET"},
				},
				Backends: []apisixv2.ApisixRouteHTTPBackend{{ServiceName: name, ServicePort: intstr.FromInt(http01SolverPort)}},
			}},
		},
	}
	current := &apisixv2.ApisixRoute{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Local.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Local.Create(ctx, desired)
		}
		return err
	}
	if err := r.checkHTTP01Ownership(current, labels, annotations); err != nil {
		return err
	}
	if reflect.DeepEqual(current.Spec, desired.Spec) {
		return nil
	}
	current.Spec = desired.Spec
	return r.Local.Update(ctx, current)
}

func (r *DownstreamReconciler) checkHTTP01Ownership(object client.Object, labels, annotations map[string]string) error {
	if object.GetLabels()[DownstreamManagedLabel] != r.SourceID ||
		object.GetLabels()[http01ChallengeID] != labels[http01ChallengeID] ||
		object.GetAnnotations()[http01ChallengeName] != annotations[http01ChallengeName] {
		return fmt.Errorf("%T %s is not managed by this HTTP-01 solver", object, client.ObjectKeyFromObject(object))
	}
	return nil
}

func (r *DownstreamReconciler) deleteHTTP01Resources(ctx context.Context, key types.NamespacedName) error {
	name := http01ResourceName(key)
	labels := map[string]string{http01ChallengeID: name}
	annotations := map[string]string{http01ChallengeName: key.Name}
	for _, object := range []client.Object{&apisixv2.ApisixRoute{}, &corev1.Service{}, &corev1.Pod{}} {
		if err := r.Local.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: name}, object); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if err := r.checkHTTP01Ownership(object, labels, annotations); err != nil {
			return err
		}
		if err := r.Local.Delete(ctx, object); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func (r *DownstreamReconciler) enqueueExistingSolvers(ctx context.Context) error {
	for _, list := range []client.ObjectList{&corev1.PodList{}, &corev1.ServiceList{}, &apisixv2.ApisixRouteList{}} {
		continueToken := ""
		for {
			if err := r.Local.List(ctx, list, client.InNamespace(r.TargetNamespace),
				client.MatchingLabels{DownstreamManagedLabel: r.SourceID},
				client.Limit(DownstreamPageSize), client.Continue(continueToken)); err != nil {
				return err
			}
			items, err := meta.ExtractList(list)
			if err != nil {
				return err
			}
			for _, item := range items {
				object, ok := item.(client.Object)
				if !ok {
					return fmt.Errorf("HTTP-01 solver list contains a non-client object")
				}
				name := object.GetAnnotations()[http01ChallengeName]
				key := types.NamespacedName{Namespace: object.GetNamespace(), Name: name}
				if name != "" && object.GetLabels()[http01ChallengeID] == http01ResourceName(key) {
					r.queue.Add(downstreamQueueKey{Kind: http01ChallengeKind, Key: key})
				}
			}
			listMeta, err := meta.ListAccessor(list)
			if err != nil {
				return err
			}
			continueToken = listMeta.GetContinue()
			if continueToken == "" {
				break
			}
		}
	}
	return nil
}
