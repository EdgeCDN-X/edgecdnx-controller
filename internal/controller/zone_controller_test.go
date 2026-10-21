package controller

import (
	"context"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestZoneReconcilerLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t.Run("creation adds finalizer and marks healthy", func(t *testing.T) {
		zone := &infrastructurev1alpha1.Zone{ObjectMeta: metav1.ObjectMeta{
			Name: "example", Namespace: "project", Finalizers: []string{"example.com/other"},
		}}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(zone).WithObjects(zone).Build()
		reconciler := &ZoneReconciler{Client: fakeClient, Scheme: scheme}
		key := client.ObjectKeyFromObject(zone)
		if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
		if err := fakeClient.Get(ctx, key, zone); err != nil {
			t.Fatal(err)
		}
		if !controllerutil.ContainsFinalizer(zone, zoneCleanupFinalizer) ||
			!controllerutil.ContainsFinalizer(zone, "example.com/other") {
			t.Fatalf("unexpected finalizers: %v", zone.Finalizers)
		}
		if zone.Status.Status != HealthStatusHealthy {
			t.Fatalf("expected Healthy, got %q", zone.Status.Status)
		}
		resourceVersion := zone.ResourceVersion
		if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatalf("healthy zone reconciliation failed without ApplicationSet support: %v", err)
		}
		if err := fakeClient.Get(ctx, key, zone); err != nil {
			t.Fatal(err)
		}
		if zone.ResourceVersion != resourceVersion {
			t.Fatal("healthy zone was unnecessarily updated")
		}
	})

	for _, endpointFinalizer := range []string{"", "example.com/endpoint-cleanup"} {
		t.Run("deletion waits for endpoint "+endpointFinalizer, func(t *testing.T) {
			zone := &infrastructurev1alpha1.Zone{ObjectMeta: metav1.ObjectMeta{
				Name: "example", Namespace: "project", DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
				Finalizers: []string{zoneCleanupFinalizer, "example.com/other"},
			}}
			endpoint := &infrastructurev1alpha1.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Name: "record", Namespace: zone.Namespace},
				Spec:       infrastructurev1alpha1.DNSEndpointSpec{Zone: &corev1.LocalObjectReference{Name: zone.Name}},
			}
			if endpointFinalizer != "" {
				endpoint.Finalizers = []string{endpointFinalizer}
			}
			otherZoneEndpoint := endpoint.DeepCopy()
			otherZoneEndpoint.Name = "other-zone"
			otherZoneEndpoint.Spec.Zone.Name = "other"
			otherNamespaceEndpoint := endpoint.DeepCopy()
			otherNamespaceEndpoint.Namespace = "other-project"
			unassignedEndpoint := endpoint.DeepCopy()
			unassignedEndpoint.Name = "unassigned"
			unassignedEndpoint.Spec.Zone = nil
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(zone, endpoint, otherZoneEndpoint, otherNamespaceEndpoint, unassignedEndpoint).Build()
			reconciler := &ZoneReconciler{Client: fakeClient, Scheme: scheme}
			key := client.ObjectKeyFromObject(zone)
			request := ctrl.Request{NamespacedName: key}
			result, err := reconciler.Reconcile(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if result.RequeueAfter == 0 {
				t.Fatal("expected requeue while endpoints are pending")
			}
			if err := fakeClient.Get(ctx, key, zone); err != nil {
				t.Fatal(err)
			}
			if !controllerutil.ContainsFinalizer(zone, zoneCleanupFinalizer) {
				t.Fatal("zone finalizer removed before confirming endpoint deletion")
			}
			endpointKey := client.ObjectKeyFromObject(endpoint)
			if endpointFinalizer != "" {
				if _, err := reconciler.Reconcile(ctx, request); err != nil {
					t.Fatal(err)
				}
				if err := fakeClient.Get(ctx, key, zone); err != nil {
					t.Fatal(err)
				}
				if !controllerutil.ContainsFinalizer(zone, zoneCleanupFinalizer) {
					t.Fatal("zone finalizer removed while endpoint is terminating")
				}
				if err := fakeClient.Get(ctx, endpointKey, endpoint); err != nil {
					t.Fatal(err)
				}
				if endpoint.DeletionTimestamp.IsZero() {
					t.Fatal("endpoint deletion was not requested")
				}
				endpoint.Finalizers = nil
				if err := fakeClient.Update(ctx, endpoint); err != nil {
					t.Fatal(err)
				}
			}
			if err := fakeClient.Get(ctx, endpointKey, endpoint); !apierrors.IsNotFound(err) {
				t.Fatalf("expected endpoint to be deleted, got %v", err)
			}
			if _, err := reconciler.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := fakeClient.Get(ctx, key, zone); err != nil {
				t.Fatal(err)
			}
			if controllerutil.ContainsFinalizer(zone, zoneCleanupFinalizer) ||
				!controllerutil.ContainsFinalizer(zone, "example.com/other") {
				t.Fatalf("unexpected finalizers after cleanup: %v", zone.Finalizers)
			}
			for _, preserved := range []*infrastructurev1alpha1.DNSEndpoint{otherZoneEndpoint, otherNamespaceEndpoint, unassignedEndpoint} {
				if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(preserved), preserved); err != nil {
					t.Fatalf("unrelated endpoint was deleted: %v", err)
				}
			}
		})
	}
}
