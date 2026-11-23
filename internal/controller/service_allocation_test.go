package controller

import (
	"context"
	"strings"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/tenancy"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSharedServiceAllocation(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, register := range []func(*runtime.Scheme) error{api.AddToScheme, coordinationv1.AddToScheme, corev1.AddToScheme, certmanagerv1.AddToScheme} {
		if err := register(scheme); err != nil {
			t.Fatal(err)
		}
	}
	project := &api.Project{ObjectMeta: metav1.ObjectMeta{Namespace: "customer", Name: "customer"}}
	zone := &api.Zone{
		ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "cdn", UID: "zone-uid"},
		Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: "platform"}, Zone: "cdn.example.com",
			Sharing: &api.ResourceSharing{Projects: []api.ProjectReference{{Namespace: "customer", Name: "customer"}}}},
	}
	service := &api.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "customer", Name: "cdn", UID: "service-one"},
		Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "customer"}, Domain: "allocated.cdn.example.com",
			RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cdn"}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, zone, service).Build()
	r := &ServiceReconciler{Client: c, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(service)}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	endpoints := &api.DNSEndpointList{}
	if err := c.List(ctx, endpoints); err != nil {
		t.Fatal(err)
	}
	if len(endpoints.Items) != 2 {
		t.Fatalf("expected A and AAAA endpoints, got %d", len(endpoints.Items))
	}
	for i := range endpoints.Items {
		endpoint := &endpoints.Items[i]
		if endpoint.Namespace != service.Namespace || endpoint.ZoneKey() != client.ObjectKeyFromObject(zone) {
			t.Fatalf("wrong ownership or zone reference: %+v", endpoint)
		}
		if err := (tenancy.Scope{Namespaced: true}).ValidateResource(ctx, c, endpoint, nil); err != nil {
			t.Fatal(err)
		}
		unmanaged := endpoint.DeepCopy()
		unmanaged.OwnerReferences = nil
		if err := tenancy.ValidateZoneUse(ctx, c, unmanaged, zone); err == nil {
			t.Fatal("unmanaged endpoint could write to shared zone")
		}
	}
	if err := c.Get(ctx, request.NamespacedName, service); err != nil {
		t.Fatal(err)
	}
	version := service.ResourceVersion
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, request.NamespacedName, service); err != nil || service.ResourceVersion != version {
		t.Fatalf("idempotent reconcile changed service: %v", err)
	}
	other := service.DeepCopy()
	other.Name, other.UID, other.ResourceVersion, other.Finalizers = "other", types.UID("service-two"), "", nil
	if err := c.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(other)}); err == nil || !strings.Contains(err.Error(), "already allocated") {
		t.Fatalf("competing hostname allocation not rejected: %v", err)
	}
	revoked := zone.DeepCopy()
	revoked.Spec.Sharing = nil
	if err := tenancy.ValidateSharing(ctx, c, revoked, zone); err == nil {
		t.Fatal("revocation accepted while service still uses zone")
	}
	if err := tenancy.ValidateInfrastructureDeletion(ctx, c, zone); err == nil {
		t.Fatal("deletion accepted while service still uses zone")
	}
	if err := c.Delete(ctx, service); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	allocation := &coordinationv1.Lease{}
	err := c.Get(ctx, types.NamespacedName{Namespace: zone.Namespace, Name: tenancy.AllocationName(service.Spec.Domain)}, allocation)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("allocation not cleaned up: %v", err)
	}
}

func TestServiceZoneSelection(t *testing.T) {
	service := &api.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "customer"},
		Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "customer"}, Domain: "host.cdn.example.com"}}
	zone := api.Zone{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "cdn"},
		Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: "platform"}, Zone: "cdn.example.com"}}
	if _, err := selectServiceZone(service, service.Spec.Domain, []api.Zone{zone}); err == nil {
		t.Fatal("private zone selected")
	}
	zone.Spec.Sharing = &api.ResourceSharing{AllProjects: true}
	if _, err := selectServiceZone(service, service.Spec.Domain, []api.Zone{zone}); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"cdn.example.com", "*.cdn.example.com", "nested.host.cdn.example.com", "alias.cdn.example.com"} {
		if _, err := selectServiceZone(service, domain, []api.Zone{zone}); err == nil {
			t.Fatalf("unallocated shared hostname selected: %q", domain)
		}
	}
	if _, err := selectServiceZone(service, service.Spec.Domain, []api.Zone{zone, zone}); err == nil {
		t.Fatal("ambiguous zones accepted")
	}
}
