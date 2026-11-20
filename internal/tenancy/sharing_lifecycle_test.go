package tenancy

import (
	"context"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	endpoint := &api.DNSEndpoint{ObjectMeta: metav1.ObjectMeta{Namespace: "customer", Name: "dns"},
		Spec: api.DNSEndpointSpec{Project: service.Spec.Project, RoutingPolicy: "Failover",
			Targets: []string{"platform/fra"}}}
	project := &api.Project{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "platform"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(project, service, endpoint, location).Build()
	ctx := context.Background()
	revoked := location.DeepCopy()
	revoked.Spec.Sharing = nil
	edited := location.DeepCopy()
	edited.Spec.Weight = 42
	edited.Labels = map[string]string{"region": "eu"}
	edited.Spec.NodeGroups[0].Nodes = []api.NodeSpec{{Name: "edge", Cname: "edge.example.com"}}
	if err := (Scope{Namespaced: true}).ValidateResource(ctx, c, edited, location); err != nil {
		t.Fatalf("ordinary shared location update rejected: %v", err)
	}
	if err := ValidateSharing(ctx, c, revoked, location); err != nil {
		t.Fatalf("location revocation blocked by consumers: %v", err)
	}
	if err := ValidateInfrastructureDeletion(ctx, c, location); err != nil {
		t.Fatalf("location deletion blocked by consumers: %v", err)
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

func TestSharedParentRevocationAllowsSelectedChildren(t *testing.T) {
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
	if err := ValidateSharing(context.Background(), c, revoked, parent); err != nil {
		t.Fatalf("shared parent revocation blocked by child consumers: %v", err)
	}
}

func TestLocationDeletionPreservesLocalReferenceGuards(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	location := &api.Location{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "fra"},
		Spec: api.LocationSpec{Project: &corev1.LocalObjectReference{Name: "platform"}}}
	for name, reference := range map[string]client.Object{
		"parent": &api.Location{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "child"},
			Spec: api.LocationSpec{Project: location.Spec.Project, Parent: "fra"}},
		"fallback": &api.Location{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "other"},
			Spec: api.LocationSpec{Project: location.Spec.Project, FallbackLocations: []string{"fra"}}},
		"prefix": &api.PrefixList{ObjectMeta: metav1.ObjectMeta{Namespace: "platform", Name: "prefix"},
			Spec: api.PrefixListSpec{Project: location.Spec.Project, Destination: "fra"}},
	} {
		t.Run(name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(location, reference).Build()
			if err := ValidateInfrastructureDeletion(context.Background(), c, location); err == nil {
				t.Fatal("local reference must still block location deletion")
			}
		})
	}
	invalid := location.DeepCopy()
	invalid.Spec.Sharing = &api.ResourceSharing{Projects: []api.ProjectReference{{Name: ""}}}
	if err := ValidateSharing(context.Background(), nil, invalid, location); err == nil {
		t.Fatal("invalid sharing must still be rejected")
	}
}
