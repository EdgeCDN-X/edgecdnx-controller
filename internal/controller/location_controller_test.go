package controller

import (
	"context"
	"reflect"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLocationReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, initialStatus := range []string{"", HealthStatusProgressing, "Degraded", HealthStatusHealthy} {
		t.Run("status="+initialStatus, func(t *testing.T) {
			location := &infrastructurev1alpha1.Location{
				ObjectMeta: metav1.ObjectMeta{Name: "location", Namespace: "project"},
				Status: infrastructurev1alpha1.LocationStatus{
					Status: initialStatus,
					NodeStatus: map[string]infrastructurev1alpha1.NodeInstanceStatus{
						"node": {},
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(location).WithObjects(location).Build()
			reconciler := &LocationReconciler{Client: fakeClient}
			key := client.ObjectKeyFromObject(location)
			if err := fakeClient.Get(ctx, key, location); err != nil {
				t.Fatal(err)
			}
			original := location.DeepCopy()
			request := ctrl.Request{NamespacedName: key}
			if _, err := reconciler.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := fakeClient.Get(ctx, key, location); err != nil {
				t.Fatal(err)
			}
			expectedStatus := original.Status
			expectedStatus.Status = HealthStatusHealthy
			if !reflect.DeepEqual(location.Status, expectedStatus) || !reflect.DeepEqual(location.Spec, original.Spec) {
				t.Fatalf("expected only health status to change, got %+v", location)
			}
			if initialStatus == HealthStatusHealthy && location.ResourceVersion != original.ResourceVersion {
				t.Fatal("already healthy location was updated")
			}
			resourceVersion := location.ResourceVersion
			if _, err := reconciler.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := fakeClient.Get(ctx, key, location); err != nil {
				t.Fatal(err)
			}
			if location.ResourceVersion != resourceVersion {
				t.Fatal("healthy location was updated on subsequent reconciliation")
			}
		})
	}

	t.Run("missing location is ignored", func(t *testing.T) {
		reconciler := &LocationReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{
			Name: "missing", Namespace: "project",
		}}); err != nil {
			t.Fatal(err)
		}
	})
}
