package controller

import (
	"context"
	"errors"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func http01TestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := downstreamTestScheme(t)
	for _, add := range []func(*runtime.Scheme) error{acmev1.AddToScheme, certv1.AddToScheme, apisixv2.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return scheme
}

func http01Fixture(t *testing.T) (*DownstreamReconciler, client.Client, client.Client, *acmev1.Challenge) {
	t.Helper()
	scheme := http01TestScheme(t)
	controller := true
	owner := func(kind, version, name, uid string) []metav1.OwnerReference {
		return []metav1.OwnerReference{{Kind: kind, APIVersion: version, Name: name, UID: types.UID(uid), Controller: &controller}}
	}
	service := &api.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "tenant", UID: "service-uid"},
		Spec: api.ServiceSpec{
			Cache:       "edge-cache",
			HostAliases: []api.HostAliasSpec{{Name: "www.example.com", TargetType: "CNAME"}},
		},
	}
	certificate := &certv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "certificate", Namespace: "tenant", UID: "cert-uid",
			Labels:          map[string]string{serviceCertificateLabel: "true", solverTargetLabel: "cname"},
			OwnerReferences: owner("Service", api.SchemeGroupVersion.String(), service.Name, string(service.UID)),
		},
		Spec: certv1.CertificateSpec{DNSNames: []string{"www.example.com"}},
	}
	request := &certv1.CertificateRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "request", Namespace: "tenant", UID: "request-uid",
		OwnerReferences: owner("Certificate", certv1.SchemeGroupVersion.String(), certificate.Name, string(certificate.UID)),
	}}
	order := &acmev1.Order{ObjectMeta: metav1.ObjectMeta{
		Name: "order", Namespace: "tenant", UID: "order-uid",
		OwnerReferences: owner("CertificateRequest", certv1.SchemeGroupVersion.String(), request.Name, string(request.UID)),
	}}
	challenge := &acmev1.Challenge{
		ObjectMeta: metav1.ObjectMeta{
			Name: "challenge", Namespace: "tenant", UID: "challenge-uid",
			OwnerReferences: owner("Order", acmev1.SchemeGroupVersion.String(), order.Name, string(order.UID)),
		},
		Spec: acmev1.ChallengeSpec{
			Type: acmev1.ACMEChallengeTypeHTTP01, DNSName: "www.example.com",
			Token: "token_123", Key: "token_123.thumbprint",
		},
		Status: acmev1.ChallengeStatus{Processing: true, State: acmev1.Pending},
	}
	upstream := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service, certificate, request, order, challenge).
		WithStatusSubresource(challenge).Build()
	local := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := newTestDownstreamReconciler(t, upstream, local, DownstreamTargetCache)
	reconciler.HTTP01SolverImage = "example/acmesolver:v1"
	return reconciler, upstream, local, challenge
}

func TestDownstreamHTTP01ReconcileAndCleanup(t *testing.T) {
	ctx := context.Background()
	r, upstream, local, challenge := http01Fixture(t)
	key := client.ObjectKeyFromObject(challenge)
	if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
		t.Fatal(err)
	}
	localKey := types.NamespacedName{Namespace: key.Namespace, Name: http01ResourceName(key)}
	pod := &corev1.Pod{}
	service := &corev1.Service{}
	route := &apisixv2.ApisixRoute{}
	for _, object := range []client.Object{pod, service, route} {
		if err := local.Get(ctx, localKey, object); err != nil {
			t.Fatalf("get %T: %v", object, err)
		}
	}
	if pod.Spec.Containers[0].Image != r.HTTP01SolverImage ||
		pod.Spec.Containers[0].Args[3] != "--key=token_123.thumbprint" {
		t.Fatalf("unexpected solver container: %#v", pod.Spec.Containers[0])
	}
	rule := route.Spec.HTTP[0]
	if route.Spec.IngressClassName != "edge-cache" || rule.Match.Hosts[0] != "www.example.com" ||
		rule.Match.Paths[0] != "/.well-known/acme-challenge/token_123" ||
		rule.Backends[0].ServiceName != localKey.Name || rule.Priority <= 0 {
		t.Fatalf("unexpected route: %#v", route.Spec)
	}
	if service.Spec.Selector[http01ChallengeID] != pod.Labels[http01ChallengeID] {
		t.Fatal("solver service does not select the solver pod")
	}
	if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
		t.Fatalf("repeat reconcile: %v", err)
	}
	challenge.Status.State = acmev1.Processing
	if err := upstream.Status().Update(ctx, challenge); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := local.Get(ctx, localKey, &apisixv2.ApisixRoute{}); err != nil {
		t.Fatalf("route removed while the CA was processing: %v", err)
	}
	challenge.Status.State = acmev1.Valid
	if err := upstream.Status().Update(ctx, challenge); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
		t.Fatal(err)
	}
	for _, object := range []client.Object{&corev1.Pod{}, &corev1.Service{}, &apisixv2.ApisixRoute{}} {
		if err := local.Get(ctx, localKey, object); !apierrors.IsNotFound(err) {
			t.Fatalf("%T not removed after challenge completion: %v", object, err)
		}
	}
}

func TestDownstreamHTTP01RequiresOwnedCNAMECertificate(t *testing.T) {
	ctx := context.Background()
	r, upstream, local, challenge := http01Fixture(t)
	key := client.ObjectKeyFromObject(challenge)
	certificate := &certv1.Certificate{}
	if err := upstream.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: "certificate"}, certificate); err != nil {
		t.Fatal(err)
	}
	certificate.OwnerReferences[0].UID = "other-service"
	if err := upstream.Update(ctx, certificate); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := local.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: http01ResourceName(key)}, &corev1.Pod{}); !apierrors.IsNotFound(err) {
		t.Fatalf("unowned challenge created a solver: %v", err)
	}
}

func TestDownstreamHTTP01RejectsUnsafeTokenAndPreservesResourcesOnUpstreamError(t *testing.T) {
	ctx := context.Background()
	r, upstream, local, challenge := http01Fixture(t)
	key := client.ObjectKeyFromObject(challenge)
	if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
		t.Fatal(err)
	}
	r.UpstreamReader = failingGetReader{Reader: upstream, err: errors.New("upstream unavailable")}
	if err := r.reconcileHTTP01Challenge(ctx, key); err == nil {
		t.Fatal("upstream error was ignored")
	}
	localKey := types.NamespacedName{Namespace: key.Namespace, Name: http01ResourceName(key)}
	if err := local.Get(ctx, localKey, &corev1.Pod{}); err != nil {
		t.Fatalf("solver removed during upstream outage: %v", err)
	}
	r.UpstreamReader = upstream
	challenge.Spec.Token = "../other"
	challenge.Spec.Key = "../other.thumbprint"
	if err := upstream.Update(ctx, challenge); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileHTTP01Challenge(ctx, key); err == nil {
		t.Fatal("unsafe token accepted")
	}
	if err := local.Get(ctx, localKey, &apisixv2.ApisixRoute{}); !apierrors.IsNotFound(err) {
		t.Fatalf("old route retained after unsafe challenge update: %v", err)
	}
}

func TestDownstreamHTTP01DoesNotOverwriteUnmanagedResources(t *testing.T) {
	ctx := context.Background()
	r, _, local, challenge := http01Fixture(t)
	key := client.ObjectKeyFromObject(challenge)
	localKey := types.NamespacedName{Namespace: key.Namespace, Name: http01ResourceName(key)}
	existing := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: localKey.Name, Namespace: localKey.Namespace}}
	if err := local.Create(ctx, existing); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileHTTP01Challenge(ctx, key); err == nil {
		t.Fatal("unmanaged pod was accepted")
	}
	if err := local.Get(ctx, localKey, &corev1.Pod{}); err != nil {
		t.Fatalf("unmanaged pod was removed: %v", err)
	}
}

func TestDownstreamHTTP01ConcurrentChallengesAndDrift(t *testing.T) {
	ctx := context.Background()
	r, upstream, local, first := http01Fixture(t)
	second := first.DeepCopy()
	second.Name = "challenge-two"
	second.UID = "challenge-two-uid"
	second.ResourceVersion = ""
	second.Spec.Token = "other"
	second.Spec.Key = "other.thumbprint"
	if err := upstream.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	firstKey, secondKey := client.ObjectKeyFromObject(first), client.ObjectKeyFromObject(second)
	for _, key := range []types.NamespacedName{firstKey, secondKey} {
		if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	secondLocalKey := types.NamespacedName{Namespace: secondKey.Namespace, Name: http01ResourceName(secondKey)}
	route := &apisixv2.ApisixRoute{}
	if err := local.Get(ctx, secondLocalKey, route); err != nil {
		t.Fatal(err)
	}
	if err := local.Delete(ctx, route); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileHTTP01Challenge(ctx, secondKey); err != nil {
		t.Fatal(err)
	}
	if err := local.Get(ctx, secondLocalKey, route); err != nil {
		t.Fatalf("deleted route was not restored: %v", err)
	}
	if err := upstream.Delete(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileHTTP01Challenge(ctx, firstKey); err != nil {
		t.Fatal(err)
	}
	if err := local.Get(ctx, secondLocalKey, &apisixv2.ApisixRoute{}); err != nil {
		t.Fatalf("other challenge was removed: %v", err)
	}
}

func TestDownstreamHTTP01ChallengeStatusIsPreserved(t *testing.T) {
	original := &acmev1.Challenge{Status: acmev1.ChallengeStatus{Processing: true, State: acmev1.Pending}}
	transformed, err := DownstreamCacheTransform(original)
	if err != nil {
		t.Fatal(err)
	}
	challenge := transformed.(*acmev1.Challenge)
	if !challenge.Status.Processing || challenge.Status.State != acmev1.Pending {
		t.Fatal("challenge status was stripped")
	}
	updated := challenge.DeepCopy()
	updated.Status.State = acmev1.Valid
	if downstreamSourceEqual(challenge, updated) {
		t.Fatal("challenge status change was ignored")
	}
}

func TestDownstreamHTTP01RecoversDeletedChallengeOnStartup(t *testing.T) {
	ctx := context.Background()
	r, upstream, local, challenge := http01Fixture(t)
	key := client.ObjectKeyFromObject(challenge)
	if err := r.reconcileHTTP01Challenge(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := upstream.Delete(ctx, challenge); err != nil {
		t.Fatal(err)
	}
	if err := r.enqueueExistingSolvers(ctx); err != nil {
		t.Fatal(err)
	}
	queued, shutdown := r.queue.Get()
	if shutdown || queued.Kind != http01ChallengeKind || queued.Key != key {
		t.Fatalf("unexpected queued challenge: %#v", queued)
	}
	r.queue.Done(queued)
	if err := r.reconcileKey(ctx, queued); err != nil {
		t.Fatal(err)
	}
	if err := local.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: http01ResourceName(key)}, &apisixv2.ApisixRoute{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphan route not removed: %v", err)
	}
}
