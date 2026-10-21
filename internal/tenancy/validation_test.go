package tenancy

import (
	"context"
	"fmt"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResourceOwnership(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &api.Project{ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "tenant"}}
	zone := &api.Zone{ObjectMeta: metav1.ObjectMeta{Name: "zone", Namespace: "tenant"}, Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: "other"}}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, zone).Build()
	scope := Scope{Namespaced: true}
	service := &api.Service{ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: "tenant"}}
	if err := scope.ValidateResource(context.Background(), reader, service, nil); err == nil {
		t.Fatal("missing project must be rejected")
	}
	service.Spec.Project = &corev1.LocalObjectReference{Name: "tenant"}
	if err := scope.ValidateResource(context.Background(), reader, service, nil); err != nil {
		t.Fatal(err)
	}
	old := service.DeepCopy()
	old.Spec.Project.Name = "other"
	if err := scope.ValidateResource(context.Background(), reader, service, old); err == nil {
		t.Fatal("project reassignment must be rejected")
	}
	endpoint := &api.DNSEndpoint{ObjectMeta: service.ObjectMeta, Spec: api.DNSEndpointSpec{Project: service.Spec.Project, Zone: &corev1.LocalObjectReference{Name: "zone"}}}
	if err := scope.ValidateResource(context.Background(), reader, endpoint, nil); err == nil {
		t.Fatal("cross-project zone must be rejected")
	}
	service.Spec.Project.Name = "missing"
	if err := (Scope{TargetNamespace: "tenant"}).ValidateResource(context.Background(), reader, service, nil); err == nil {
		t.Fatal("nonexistent project must be rejected")
	}
}

func TestSharedResourceOwnership(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	metadata := metav1.ObjectMeta{Name: "resource", Namespace: "edgecdnx"}
	reference := &corev1.LocalObjectReference{Name: "tenant-one"}
	project := &api.Project{ObjectMeta: metav1.ObjectMeta{Name: reference.Name, Namespace: metadata.Namespace}}
	location := &api.Location{ObjectMeta: metav1.ObjectMeta{Name: "destination", Namespace: metadata.Namespace}, Spec: api.LocationSpec{Project: reference}}
	zone := &api.Zone{ObjectMeta: metav1.ObjectMeta{Name: "zone", Namespace: metadata.Namespace}, Spec: api.ZoneSpec{Project: reference}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, location, zone).Build()
	scope := Scope{TargetNamespace: metadata.Namespace}
	for _, object := range []client.Object{
		&api.APIKey{ObjectMeta: metadata, Spec: api.APIKeySpec{Project: reference}},
		&api.DNSEndpoint{ObjectMeta: metadata, Spec: api.DNSEndpointSpec{Project: reference, Zone: &corev1.LocalObjectReference{Name: zone.Name}}},
		&api.HealthCheckProfile{ObjectMeta: metadata, Spec: api.HealthCheckProfileSpec{Project: reference}},
		&api.Location{ObjectMeta: metadata, Spec: api.LocationSpec{Project: reference, Parent: location.Name}},
		&api.PrefixList{ObjectMeta: metadata, Spec: api.PrefixListSpec{Project: reference, Destination: location.Name}},
		&api.Service{ObjectMeta: metadata, Spec: api.ServiceSpec{Project: reference}},
		&api.Zone{ObjectMeta: metadata, Spec: api.ZoneSpec{Project: reference}},
	} {
		t.Run(fmt.Sprintf("%T", object), func(t *testing.T) {
			if err := scope.ValidateResource(context.Background(), reader, object, nil); err != nil {
				t.Fatal(err)
			}
			if err := (Scope{Namespaced: true}).ValidateResource(context.Background(), reader, object, nil); err == nil {
				t.Fatal("dedicated placement must reject a project name different from the namespace")
			}
			object.SetNamespace("outside")
			if err := scope.ValidateResource(context.Background(), reader, object, nil); err == nil {
				t.Fatal("shared placement must reject other namespaces")
			}
		})
	}
	for _, name := range []string{"tenant-one", "tenant-two"} {
		if err := scope.ValidateProject("edgecdnx", name); err != nil {
			t.Fatalf("shared namespace must allow multiple projects: %v", err)
		}
	}
}
