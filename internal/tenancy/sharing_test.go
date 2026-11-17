package tenancy

import (
	"context"
	"strings"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSharedZoneConsumption(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*api.DNSEndpoint, *api.Zone, *api.Service, *coordinationv1.Lease)
		wantError string
	}{
		{name: "allocated service"},
		{name: "private zone", change: func(_ *api.DNSEndpoint, zone *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			zone.Spec.Sharing = nil
		}, wantError: "not shared"},
		{name: "same project name different namespace", change: func(_ *api.DNSEndpoint, zone *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			zone.Spec.Sharing = &api.ResourceSharing{Projects: []api.ProjectReference{{Name: "customer", Namespace: "other"}}}
		}, wantError: "not shared"},
		{name: "unmanaged record", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			endpoint.OwnerReferences = nil
		}, wantError: "service-managed"},
		{name: "stale service UID", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			endpoint.OwnerReferences[0].UID = "old-service"
		}, wantError: "live Service"},
		{name: "another service hostname", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			endpoint.Spec.DNSName = "another.cdn.example.com"
		}, wantError: "allocated single-label hostname"},
		{name: "apex", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, service *api.Service, _ *coordinationv1.Lease) {
			endpoint.Spec.DNSName, service.Spec.Domain = "cdn.example.com", "cdn.example.com"
		}, wantError: "allocated single-label hostname"},
		{name: "wildcard", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, service *api.Service, _ *coordinationv1.Lease) {
			endpoint.Spec.DNSName, service.Spec.Domain = "*.cdn.example.com", "*.cdn.example.com"
		}, wantError: "allocated single-label hostname"},
		{name: "arbitrary TXT", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			endpoint.Spec.RecordType = "TXT"
		}, wantError: "Geolocation A or AAAA"},
		{name: "arbitrary address", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			endpoint.Spec.Targets = []string{"192.0.2.1"}
		}, wantError: "Geolocation A or AAAA"},
		{name: "modified selector", change: func(endpoint *api.DNSEndpoint, _ *api.Zone, _ *api.Service, _ *coordinationv1.Lease) {
			endpoint.Spec.RouteSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "other"}}
		}, wantError: "routing must match"},
		{name: "foreign allocation", change: func(_ *api.DNSEndpoint, _ *api.Zone, _ *api.Service, allocation *coordinationv1.Lease) {
			owner := "other-service"
			allocation.Spec.HolderIdentity = &owner
		}, wantError: "not allocated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			for _, register := range []func(*runtime.Scheme) error{api.AddToScheme, coordinationv1.AddToScheme} {
				if err := register(scheme); err != nil {
					t.Fatal(err)
				}
			}
			zone := &api.Zone{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "cdn"},
				Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: "platform"}, Zone: "cdn.example.com",
					Sharing: &api.ResourceSharing{AllProjects: true}}}
			service := &api.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "customer", Name: "service", UID: "service-uid"},
				Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "customer"}, Domain: "allocated.cdn.example.com",
					RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cdn"}}}}
			controlled := true
			endpoint := &api.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: "record", OwnerReferences: []metav1.OwnerReference{{
					Kind: "Service", APIVersion: api.SchemeGroupVersion.String(), Name: service.Name, UID: service.UID, Controller: &controlled,
				}}},
				Spec: api.DNSEndpointSpec{Project: service.Spec.Project, Zone: &corev1.ObjectReference{Name: zone.Name, Namespace: zone.Namespace},
					DNSName: service.Spec.Domain, RecordType: "A", RoutingPolicy: "Geolocation", RouteSelector: service.Spec.RouteSelector.DeepCopy()},
			}
			identity := string(service.UID)
			allocation := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Namespace: zone.Namespace, Name: AllocationName(service.Spec.Domain),
					Annotations: map[string]string{AllocationDomainAnnotation: "allocated.cdn.example.com."}},
				Spec: coordinationv1.LeaseSpec{HolderIdentity: &identity},
			}
			if tt.change != nil {
				tt.change(endpoint, zone, service, allocation)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(zone, service, allocation).Build()
			err := ValidateZoneUse(context.Background(), c, endpoint, zone)
			if tt.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("expected %q, got %v", tt.wantError, err)
			}
		})
	}
}
