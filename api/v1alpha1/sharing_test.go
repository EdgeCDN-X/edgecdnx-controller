package v1alpha1

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCanUseResource(t *testing.T) {
	tests := []struct {
		name                           string
		sourceNamespace, sourceProject string
		sharing                        *ResourceSharing
		want                           bool
	}{
		{name: "owner", sourceNamespace: "platform", sourceProject: "platform", want: true},
		{name: "private", sourceNamespace: "tenant", sourceProject: "tenant"},
		{name: "all projects", sourceNamespace: "tenant", sourceProject: "tenant", sharing: &ResourceSharing{AllProjects: true}, want: true},
		{name: "explicit project", sourceNamespace: "tenant", sourceProject: "customer", sharing: &ResourceSharing{Projects: []ProjectReference{{Namespace: "tenant", Name: "customer"}}}, want: true},
		{name: "wrong namespace", sourceNamespace: "other", sourceProject: "customer", sharing: &ResourceSharing{Projects: []ProjectReference{{Namespace: "tenant", Name: "customer"}}}},
		{name: "default namespace", sourceNamespace: "platform", sourceProject: "customer", sharing: &ResourceSharing{Projects: []ProjectReference{{Name: "customer"}}}, want: true},
		{name: "missing identity", sharing: &ResourceSharing{AllProjects: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanUseResource(tt.sourceNamespace, tt.sourceProject, "platform", "platform", tt.sharing); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLegacyZoneReference(t *testing.T) {
	endpoint := &DNSEndpoint{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}}
	if err := json.Unmarshal([]byte(`{"spec":{"zone":{"name":"example"}}}`), endpoint); err != nil {
		t.Fatal(err)
	}
	if got := endpoint.ZoneKey(); got != (types.NamespacedName{Namespace: "tenant", Name: "example"}) {
		t.Fatalf("legacy reference changed: %v", got)
	}
	endpoint.Spec.Zone.Namespace = "platform"
	if got := endpoint.ZoneKey(); got.Namespace != "platform" {
		t.Fatalf("explicit namespace ignored: %v", got)
	}
}

func TestValidateResourceSharing(t *testing.T) {
	for _, sharing := range []*ResourceSharing{
		{Projects: []ProjectReference{{Name: ""}}},
		{Projects: []ProjectReference{{Name: "customer", Namespace: "INVALID"}}},
		{Projects: []ProjectReference{{Name: "customer"}, {Name: "customer", Namespace: "platform"}}},
	} {
		if err := ValidateResourceSharing(sharing, "platform"); err == nil {
			t.Fatalf("invalid sharing accepted: %+v", sharing)
		}
	}
}
