package builder

import (
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDNSEndpointBuilderWithService(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"edgecdnx.com/routing-instance": "edgecdnx"}}
	service := infrastructurev1alpha1.Service{
		Spec: infrastructurev1alpha1.ServiceSpec{RouteSelector: selector},
	}
	builder := NewDefaultDNSEndpointBuilder("example-com-a", "default")
	builder.WithService(service, "example.com", "A")

	dnsEndpoint, hash, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}

	if dnsEndpoint.Spec.DNSName != "example.com" {
		t.Errorf("DNSName = %q, want %q", dnsEndpoint.Spec.DNSName, "example.com")
	}
	if dnsEndpoint.Spec.RecordType != "A" {
		t.Errorf("RecordType = %q, want %q", dnsEndpoint.Spec.RecordType, "A")
	}
	if dnsEndpoint.Spec.RoutingPolicy != DNSRoutingPolicyGeolocation {
		t.Errorf("RoutingPolicy = %q, want %q", dnsEndpoint.Spec.RoutingPolicy, DNSRoutingPolicyGeolocation)
	}
	if dnsEndpoint.Spec.Targets != nil {
		t.Errorf("Targets = %v, want nil", dnsEndpoint.Spec.Targets)
	}
	if dnsEndpoint.Spec.RouteSelector == selector {
		t.Error("RouteSelector was not deep-copied")
	}
	if dnsEndpoint.Annotations[ValuesHashAnnotation] != hash {
		t.Errorf("hash annotation = %q, want %q", dnsEndpoint.Annotations[ValuesHashAnnotation], hash)
	}
}
