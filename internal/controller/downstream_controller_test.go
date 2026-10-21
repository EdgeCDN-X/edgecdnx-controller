package controller

import (
	"context"
	"errors"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type failingGetReader struct {
	client.Reader
	err error
}

func (c failingGetReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return c.err
}

func downstreamTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func newTestDownstreamReconciler(t *testing.T, upstream client.Reader, local client.Client, target DownstreamTarget) *DownstreamReconciler {
	t.Helper()
	kinds, err := downstreamResourceKinds(target)
	if err != nil {
		t.Fatal(err)
	}
	kindMap := make(map[string]downstreamResourceKind, len(kinds))
	for _, kind := range kinds {
		kindMap[kind.name] = kind
	}
	return &DownstreamReconciler{
		UpstreamReader: upstream,
		Local:          local,
		Target:         target,
		SourceID:       "source-a",
		Workers:        1,
		queue:          workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[downstreamQueueKey]()),
		kinds:          kindMap,
	}
}

func TestDownstreamResourceKinds(t *testing.T) {
	tests := []struct {
		target DownstreamTarget
		want   []string
	}{
		{target: DownstreamTargetRouter, want: []string{"Location", "HealthCheckProfile", "Zone", "PrefixList", "DNSEndpoint"}},
		{target: DownstreamTargetCache, want: []string{"Service"}},
	}
	for _, test := range tests {
		kinds, err := downstreamResourceKinds(test.target)
		if err != nil {
			t.Fatalf("downstreamResourceKinds(%q): %v", test.target, err)
		}
		if len(kinds) != len(test.want) {
			t.Fatalf("target %q returned %d kinds, want %d", test.target, len(kinds), len(test.want))
		}
		for i := range test.want {
			if kinds[i].name != test.want[i] {
				t.Errorf("target %q kind %d = %q, want %q", test.target, i, kinds[i].name, test.want[i])
			}
		}
	}
	if _, err := downstreamResourceKinds("invalid"); err == nil {
		t.Fatal("invalid target should be rejected")
	}
}

func TestDownstreamSourceEqualIgnoresStatusOnlyChanges(t *testing.T) {
	oldLocation := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{Name: "north", Namespace: "locations"},
		Spec:       infrastructurev1alpha1.LocationSpec{Parent: "europe"},
		Status:     infrastructurev1alpha1.LocationStatus{Status: "Healthy"},
	}
	newLocation := oldLocation.DeepCopy()
	newLocation.Status.Status = "Degraded"
	if !downstreamSourceEqual(oldLocation, newLocation) {
		t.Fatal("status-only changes should not enqueue a downstream update")
	}
	newLocation.Spec.Parent = "americas"
	if downstreamSourceEqual(oldLocation, newLocation) {
		t.Fatal("spec changes should enqueue a downstream update")
	}
}

func TestDownstreamCacheTransformDropsStatusAndManagedFields(t *testing.T) {
	location := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{
			Name:          "north",
			ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "test"}},
		},
		Status: infrastructurev1alpha1.LocationStatus{Status: "Healthy"},
	}
	transformed, err := DownstreamCacheTransform(location)
	if err != nil {
		t.Fatalf("DownstreamCacheTransform(): %v", err)
	}
	result := transformed.(*infrastructurev1alpha1.Location)
	if result.Status.Status != "" {
		t.Errorf("cached status was retained: %q", result.Status.Status)
	}
	if len(result.ManagedFields) != 0 {
		t.Errorf("managed fields were retained: %#v", result.ManagedFields)
	}
}

func TestEnqueueExistingMirrorsSelectsCurrentSource(t *testing.T) {
	ctx := context.Background()
	scheme := downstreamTestScheme(t)
	owned := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned",
			Namespace: "locations",
			Labels:    map[string]string{DownstreamManagedLabel: "source-a"},
		},
	}
	otherSource := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other",
			Namespace: "locations",
			Labels:    map[string]string{DownstreamManagedLabel: "source-b"},
		},
	}
	local := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owned, otherSource).Build()
	reconciler := newTestDownstreamReconciler(t, fake.NewClientBuilder().WithScheme(scheme).Build(), local, DownstreamTargetRouter)

	if err := reconciler.enqueueExistingMirrors(ctx); err != nil {
		t.Fatalf("enqueueExistingMirrors(): %v", err)
	}
	key, shutdown := reconciler.queue.Get()
	if shutdown {
		t.Fatal("queue unexpectedly shut down")
	}
	reconciler.queue.Done(key)
	if key.Kind != "Location" || key.Key != client.ObjectKeyFromObject(owned) {
		t.Errorf("queued key = %#v, want owned Location", key)
	}
	if reconciler.queue.Len() != 0 {
		t.Errorf("queued %d resources, expected only the current source's mirror", reconciler.queue.Len())
	}
}

func TestDownstreamSharedScopePreservesOtherNamespaces(t *testing.T) {
	ctx := context.Background()
	scheme := downstreamTestScheme(t)
	mirror := &infrastructurev1alpha1.Location{ObjectMeta: metav1.ObjectMeta{
		Name: "outside", Namespace: "other", Labels: map[string]string{DownstreamManagedLabel: "source-a"},
	}}
	local := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mirror).Build()
	reconciler := newTestDownstreamReconciler(t, fake.NewClientBuilder().WithScheme(scheme).Build(), local, DownstreamTargetRouter)
	reconciler.TargetNamespace = "edgecdnx"
	if err := reconciler.enqueueExistingMirrors(ctx); err != nil {
		t.Fatal(err)
	}
	if reconciler.queue.Len() != 0 {
		t.Fatal("out-of-scope mirrors must not be queued")
	}
	if err := reconciler.reconcileKey(ctx, downstreamQueueKey{Kind: "Location", Key: client.ObjectKeyFromObject(mirror)}); err != nil {
		t.Fatal(err)
	}
	if err := local.Get(ctx, client.ObjectKeyFromObject(mirror), &infrastructurev1alpha1.Location{}); err != nil {
		t.Fatalf("out-of-scope mirror was deleted: %v", err)
	}
}

func TestDownstreamSyncPreservesNamespaceAndLocalStatus(t *testing.T) {
	ctx := context.Background()
	scheme := downstreamTestScheme(t)
	source := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "north",
			Namespace:       "locations",
			UID:             types.UID("upstream-uid"),
			ResourceVersion: "17",
			OwnerReferences: []metav1.OwnerReference{{Name: "upstream-owner", Kind: "Location"}},
		},
		Spec:   infrastructurev1alpha1.LocationSpec{Parent: "europe"},
		Status: infrastructurev1alpha1.LocationStatus{Status: "Healthy"},
	}
	upstream := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source).Build()
	local := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := newTestDownstreamReconciler(t, upstream, local, DownstreamTargetRouter)

	key := types.NamespacedName{Namespace: "locations", Name: "north"}
	queueKey := downstreamQueueKey{Kind: "Location", Key: key}
	if err := reconciler.reconcileKey(ctx, queueKey); err != nil {
		t.Fatalf("reconcileKey(): %v", err)
	}

	localLocation := &infrastructurev1alpha1.Location{}
	if err := local.Get(ctx, key, localLocation); err != nil {
		t.Fatalf("get mirrored Location: %v", err)
	}
	if localLocation.Spec.Parent != "europe" {
		t.Errorf("mirrored spec parent = %q", localLocation.Spec.Parent)
	}
	if localLocation.Status.Status != "" {
		t.Errorf("upstream status was copied: %q", localLocation.Status.Status)
	}
	if localLocation.UID == types.UID("upstream-uid") || localLocation.ResourceVersion == "17" {
		t.Error("upstream Kubernetes identity metadata was copied")
	}
	if len(localLocation.OwnerReferences) != 0 {
		t.Errorf("upstream owner references were copied: %#v", localLocation.OwnerReferences)
	}
	if localLocation.Labels[DownstreamManagedLabel] != "source-a" {
		t.Errorf("source marker = %q", localLocation.Labels[DownstreamManagedLabel])
	}
	namespace := &corev1.Namespace{}
	if err := local.Get(ctx, types.NamespacedName{Name: "locations"}, namespace); err != nil {
		t.Errorf("namespace was not created: %v", err)
	}

	localLocation.Status.Status = "Healthy"
	if err := local.Update(ctx, localLocation); err != nil {
		t.Fatalf("set local status: %v", err)
	}
	source.Spec.Parent = "americas"
	if err := upstream.Update(ctx, source); err != nil {
		t.Fatalf("update upstream Location: %v", err)
	}
	if err := reconciler.reconcileKey(ctx, queueKey); err != nil {
		t.Fatalf("reconcileKey() after upstream update: %v", err)
	}
	if err := local.Get(ctx, key, localLocation); err != nil {
		t.Fatalf("get updated mirror: %v", err)
	}
	if localLocation.Spec.Parent != "americas" {
		t.Errorf("upstream update was not applied: parent = %q", localLocation.Spec.Parent)
	}
	if localLocation.Status.Status != "Healthy" {
		t.Errorf("local status was overwritten by upstream sync: %q", localLocation.Status.Status)
	}
}

func TestDownstreamSyncPrunesDeletedUpstreamResources(t *testing.T) {
	ctx := context.Background()
	scheme := downstreamTestScheme(t)
	localLocation := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "removed-upstream",
			Namespace: "locations",
			Labels: map[string]string{
				DownstreamManagedLabel: "source-a",
			},
		},
	}
	unmanagedLocation := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{Name: "local-only", Namespace: "locations"},
	}
	upstream := fake.NewClientBuilder().WithScheme(scheme).Build()
	local := fake.NewClientBuilder().WithScheme(scheme).WithObjects(localLocation, unmanagedLocation).Build()
	reconciler := newTestDownstreamReconciler(t, upstream, local, DownstreamTargetRouter)

	if err := reconciler.reconcileKey(ctx, downstreamQueueKey{Kind: "Location", Key: client.ObjectKeyFromObject(localLocation)}); err != nil {
		t.Fatalf("reconcileKey(): %v", err)
	}
	if err := local.Get(ctx, client.ObjectKeyFromObject(localLocation), &infrastructurev1alpha1.Location{}); err == nil {
		t.Fatal("stale managed mirror was not deleted")
	}
	if err := local.Get(ctx, client.ObjectKeyFromObject(unmanagedLocation), &infrastructurev1alpha1.Location{}); err != nil {
		t.Fatalf("unmanaged local resource was deleted: %v", err)
	}
}

func TestDownstreamSyncDoesNotPruneWhenUpstreamListFails(t *testing.T) {
	ctx := context.Background()
	scheme := downstreamTestScheme(t)
	localLocation := &infrastructurev1alpha1.Location{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keep-during-outage",
			Namespace: "locations",
			Labels: map[string]string{
				DownstreamManagedLabel: "source-a",
			},
		},
	}
	local := fake.NewClientBuilder().WithScheme(scheme).WithObjects(localLocation).Build()
	upstream := failingGetReader{Reader: fake.NewClientBuilder().WithScheme(scheme).Build(), err: errors.New("upstream unavailable")}
	reconciler := newTestDownstreamReconciler(t, upstream, local, DownstreamTargetRouter)

	if err := reconciler.reconcileKey(ctx, downstreamQueueKey{Kind: "Location", Key: client.ObjectKeyFromObject(localLocation)}); err == nil {
		t.Fatal("reconcileKey() should report the upstream failure")
	}
	if err := local.Get(ctx, client.ObjectKeyFromObject(localLocation), &infrastructurev1alpha1.Location{}); err != nil {
		t.Fatalf("mirror was pruned during upstream outage: %v", err)
	}
}
