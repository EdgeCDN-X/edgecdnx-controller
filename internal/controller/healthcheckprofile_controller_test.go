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

func TestHealthCheckProfileReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, initialStatus := range []string{"", HealthStatusProgressing, "Degraded", HealthStatusHealthy} {
		t.Run("status="+initialStatus, func(t *testing.T) {
			profile := &infrastructurev1alpha1.HealthCheckProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "profile", Namespace: "project"},
				Status:     infrastructurev1alpha1.HealthCheckProfileStatus{Status: initialStatus},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(profile).WithObjects(profile).Build()
			reconciler := &HealthCheckProfileReconciler{Client: fakeClient}
			key := client.ObjectKeyFromObject(profile)
			if err := fakeClient.Get(ctx, key, profile); err != nil {
				t.Fatal(err)
			}
			original := profile.DeepCopy()
			request := ctrl.Request{NamespacedName: key}
			if _, err := reconciler.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := fakeClient.Get(ctx, key, profile); err != nil {
				t.Fatal(err)
			}
			if profile.Status.Status != HealthStatusHealthy || !reflect.DeepEqual(profile.Spec, original.Spec) {
				t.Fatalf("expected only health status to change, got %+v", profile)
			}
			if initialStatus == HealthStatusHealthy && profile.ResourceVersion != original.ResourceVersion {
				t.Fatal("already healthy profile was updated")
			}
			resourceVersion := profile.ResourceVersion
			if _, err := reconciler.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := fakeClient.Get(ctx, key, profile); err != nil {
				t.Fatal(err)
			}
			if profile.ResourceVersion != resourceVersion {
				t.Fatal("healthy profile was updated on subsequent reconciliation")
			}
		})
	}

	t.Run("deleting profile is ignored", func(t *testing.T) {
		now := metav1.Now()
		profile := &infrastructurev1alpha1.HealthCheckProfile{ObjectMeta: metav1.ObjectMeta{
			Name: "profile", Namespace: "project", DeletionTimestamp: &now,
			Finalizers: []string{"example.com/cleanup"},
		}}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(profile).WithObjects(profile).Build()
		reconciler := &HealthCheckProfileReconciler{Client: fakeClient}
		key := client.ObjectKeyFromObject(profile)
		if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
		if err := fakeClient.Get(ctx, key, profile); err != nil {
			t.Fatal(err)
		}
		if profile.Status.Status != "" {
			t.Fatal("deleting profile status was changed")
		}
	})

	t.Run("missing profile is ignored", func(t *testing.T) {
		reconciler := &HealthCheckProfileReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{
			Name: "missing", Namespace: "project",
		}}); err != nil {
			t.Fatal(err)
		}
	})
}
