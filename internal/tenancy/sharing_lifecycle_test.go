package tenancy

import (
	"context"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSharedLocationLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	service := &api.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "customer", Name: "cdn"},
		Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "customer"},
			RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cdn"}}}}
	location := &api.Location{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "fra"},
		Spec: api.LocationSpec{Project: &corev1.LocalObjectReference{Name: "platform"},
			Sharing:    &api.ResourceSharing{AllProjects: true},
			NodeGroups: []api.NodeGroupSpec{{Name: "edge", Labels: map[string]string{"pool": "cdn"}}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service, location).Build()
	ctx := context.Background()
	revoked := location.DeepCopy()
	revoked.Spec.Sharing = nil
	if err := ValidateSharing(ctx, c, revoked, location); err == nil {
		t.Fatal("revocation accepted while shared node group selected")
	}
	if err := ValidateInfrastructureDeletion(ctx, c, location); err == nil {
		t.Fatal("deletion accepted while shared node group selected")
	}
	service.Spec.RouteSelector.MatchLabels["pool"] = "other"
	if err := c.Update(ctx, service); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSharing(ctx, c, revoked, location); err != nil {
		t.Fatalf("unused sharing could not be revoked: %v", err)
	}
	if err := ValidateInfrastructureDeletion(ctx, c, location); err != nil {
		t.Fatalf("unused location could not be deleted: %v", err)
	}
}

func TestSharedParentRevocationProtectsSelectedChildren(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &corev1.LocalObjectReference{Name: "platform"}
	parent := &api.Location{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "region"},
		Spec: api.LocationSpec{Project: project, Sharing: &api.ResourceSharing{AllProjects: true}}}
	child := &api.Location{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "fra"},
		Spec: api.LocationSpec{Project: project, Parent: parent.Name,
			NodeGroups: []api.NodeGroupSpec{{Name: "edge", Labels: map[string]string{"pool": "cdn"}}}}}
	service := &api.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "customer", Name: "cdn"},
		Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "customer"},
			RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cdn"}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, child, service).Build()
	revoked := parent.DeepCopy()
	revoked.Spec.Sharing = nil
	if err := ValidateSharing(context.Background(), c, revoked, parent); err == nil {
		t.Fatal("shared parent revoked while its private child is selected")
	}
}
