package controller

import (
	"context"
	"strings"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestServiceDNSEndpoints(t *testing.T) {
	tests := []struct {
		name, targetType, alias, zoneProject, zoneNamespace string
		wantAlias                                           bool
		wantError                                           string
		deletingZone, ambiguousZone                         bool
	}{
		{name: "delegated subdomain", targetType: "DELEGATION", alias: "www.example.com", zoneProject: "customer", zoneNamespace: "customer", wantAlias: true},
		{name: "delegated apex", targetType: "DELEGATION", alias: "example.com", zoneProject: "customer", zoneNamespace: "customer", wantAlias: true},
		{name: "case insensitive delegation", targetType: "DELEGATION", alias: "WWW.Example.COM.", zoneProject: "customer", zoneNamespace: "customer", wantAlias: true},
		{name: "deleted delegation zone", targetType: "DELEGATION", alias: "www.example.com", zoneProject: "customer", zoneNamespace: "customer", deletingZone: true, wantError: "no matching zone in the service project"},
		{name: "deleted legacy zone", alias: "www.example.com", zoneProject: "customer", zoneNamespace: "customer", deletingZone: true},
		{name: "ambiguous legacy zone", alias: "www.example.com", zoneProject: "customer", zoneNamespace: "customer", ambiguousZone: true, wantError: "ambiguous"},
		{name: "delegation needs owned zone", targetType: "DELEGATION", alias: "www.example.com", zoneProject: "platform", zoneNamespace: "customer", wantError: "no matching zone in the service project"},
		{name: "delegation cannot use remote zone", targetType: "DELEGATION", alias: "www.example.com", zoneProject: "customer", zoneNamespace: "other", wantError: "no matching zone in the service project"},
		{name: "delegation needs covering zone", targetType: "DELEGATION", alias: "notexample.com", zoneProject: "customer", zoneNamespace: "customer", wantError: "no matching zone in the service project"},
		{name: "external CNAME without zone", targetType: "CNAME", alias: "www.external.test"},
		{name: "CNAME even with owned zone", targetType: "CNAME", alias: "www.example.com", zoneProject: "customer", zoneNamespace: "customer"},
		{name: "legacy owned zone", alias: "www.example.com", zoneProject: "customer", zoneNamespace: "customer", wantAlias: true},
		{name: "legacy external domain", alias: "www.external.test"},
		{name: "legacy foreign zone", alias: "www.example.com", zoneProject: "platform", zoneNamespace: "platform"},
		{name: "no host aliases"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			for _, register := range []func(*runtime.Scheme) error{api.AddToScheme, coordinationv1.AddToScheme, corev1.AddToScheme, certmanagerv1.AddToScheme} {
				if err := register(scheme); err != nil {
					t.Fatal(err)
				}
			}
			service := &api.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: "customer", UID: "service-uid"},
				Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "customer"}, Domain: "allocated.cdn.test",
					RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cdn"}}},
			}
			if tt.alias != "" {
				service.Spec.HostAliases = []api.HostAliasSpec{{Name: tt.alias, TargetType: tt.targetType}}
			}
			primaryZone := &api.Zone{
				ObjectMeta: metav1.ObjectMeta{Name: "cdn", Namespace: "platform", UID: "zone-uid"},
				Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: "platform"}, Zone: "cdn.test",
					Sharing: &api.ResourceSharing{AllProjects: true}},
			}
			objects := []client.Object{service, primaryZone}
			if tt.zoneProject != "" {
				zone := &api.Zone{
					ObjectMeta: metav1.ObjectMeta{Name: "customer-zone", Namespace: tt.zoneNamespace},
					Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: tt.zoneProject}, Zone: "example.com",
						Sharing: &api.ResourceSharing{AllProjects: true}},
				}
				if tt.deletingZone {
					now := metav1.Now()
					zone.DeletionTimestamp = &now
					zone.Finalizers = []string{"test/keep"}
				}
				objects = append(objects, zone)
				if tt.ambiguousZone {
					other := zone.DeepCopy()
					other.Name = "other-zone"
					objects = append(objects, other)
				}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			reconciler := &ServiceReconciler{Client: c, Scheme: scheme}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(service)}
			_, err := reconciler.Reconcile(ctx, request)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("want %q, got %v", tt.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			endpoints := &api.DNSEndpointList{}
			if err := c.List(ctx, endpoints); err != nil {
				t.Fatal(err)
			}
			wantCount := 2
			if tt.wantAlias {
				wantCount = 4
			}
			if len(endpoints.Items) != wantCount {
				t.Fatalf("got %d endpoints, want %d", len(endpoints.Items), wantCount)
			}
			for i := range endpoints.Items {
				endpoint := &endpoints.Items[i]
				wantZone := client.ObjectKeyFromObject(primaryZone)
				if endpoint.Spec.DNSName == tt.alias {
					wantZone = client.ObjectKey{Namespace: service.Namespace, Name: "customer-zone"}
				}
				if endpoint.ZoneKey() != wantZone || endpoint.Namespace != service.Namespace ||
					endpoint.Spec.Project.Name != service.Spec.Project.Name || !metav1.IsControlledBy(endpoint, service) {
					t.Fatalf("incorrect ownership or reference: %+v", endpoint)
				}
				if endpoint.Spec.RecordType != "A" && endpoint.Spec.RecordType != "AAAA" {
					t.Fatalf("unexpected record type: %s", endpoint.Spec.RecordType)
				}
			}
			if tt.wantAlias {
				if err := c.Get(ctx, request.NamespacedName, service); err != nil {
					t.Fatal(err)
				}
				service.Spec.HostAliases[0].TargetType = "CNAME"
				if err := c.Update(ctx, service); err != nil {
					t.Fatal(err)
				}
				if _, err := reconciler.Reconcile(ctx, request); err != nil {
					t.Fatal(err)
				}
				if err := c.List(ctx, endpoints); err != nil {
					t.Fatal(err)
				}
				if len(endpoints.Items) != 2 {
					t.Fatalf("switching to external CNAME left stale endpoints: %d", len(endpoints.Items))
				}
			}
		})
	}
}
