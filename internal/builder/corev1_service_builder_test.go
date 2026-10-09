package builder

import (
	"net"
	"testing"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

func TestCoreV1ServiceBuilderWithUpstreamFQDN(t *testing.T) {
	b := NewDefaultCoreV1ServiceBuilder("origin", "default")
	b.WithUpstream("origin.example.com", 443)

	svc, _, err := b.Build()
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	if svc.Spec.Type != v1.ServiceTypeExternalName {
		t.Errorf("Type = %q, want %q", svc.Spec.Type, v1.ServiceTypeExternalName)
	}
	if svc.Spec.ExternalName != "origin.example.com" {
		t.Errorf("ExternalName = %q, want %q", svc.Spec.ExternalName, "origin.example.com")
	}
}

func TestCoreV1ServiceBuilderWithUpstreamIP(t *testing.T) {
	for _, upstream := range []string{"203.0.113.10", "2001:db8::10"} {
		b := NewDefaultCoreV1ServiceBuilder("origin", "default")
		b.WithUpstream(upstream, 8443)

		svc, _, err := b.Build()
		if err != nil {
			t.Fatalf("%s: Build() returned an error: %v", upstream, err)
		}
		if svc.Spec.Type != v1.ServiceTypeClusterIP || svc.Spec.ClusterIP != v1.ClusterIPNone {
			t.Errorf("%s: want headless ClusterIP Service, got type=%q clusterIP=%q", upstream, svc.Spec.Type, svc.Spec.ClusterIP)
		}
		if svc.Spec.ExternalName != "" {
			t.Errorf("%s: ExternalName = %q, want empty", upstream, svc.Spec.ExternalName)
		}
		if svc.Spec.Selector != nil {
			t.Errorf("%s: Selector = %v, want nil (endpoints come from an EndpointSlice)", upstream, svc.Spec.Selector)
		}
		if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 8443 || svc.Spec.Ports[0].Name != OriginPortName {
			t.Errorf("%s: Ports = %+v, want one port %q/8443", upstream, svc.Spec.Ports, OriginPortName)
		}
	}
}

func TestEndpointSliceBuilderWithIPUpstream(t *testing.T) {
	cases := []struct {
		ip       string
		wantType discoveryv1.AddressType
	}{
		{"203.0.113.10", discoveryv1.AddressTypeIPv4},
		{"2001:db8::10", discoveryv1.AddressTypeIPv6},
	}
	for _, c := range cases {
		b := NewEndpointSliceBuilder("origin-origin", "default")
		b.WithIPUpstream("origin", net.ParseIP(c.ip), 443)

		slice, hash, err := b.Build()
		if err != nil {
			t.Fatalf("%s: Build() returned an error: %v", c.ip, err)
		}
		if slice.AddressType != c.wantType {
			t.Errorf("%s: AddressType = %q, want %q", c.ip, slice.AddressType, c.wantType)
		}
		if slice.Labels[discoveryv1.LabelServiceName] != "origin" {
			t.Errorf("%s: service-name label = %q, want %q", c.ip, slice.Labels[discoveryv1.LabelServiceName], "origin")
		}
		if slice.Labels[discoveryv1.LabelManagedBy] != EndpointSliceManagedBy {
			t.Errorf("%s: managed-by label = %q, want %q", c.ip, slice.Labels[discoveryv1.LabelManagedBy], EndpointSliceManagedBy)
		}
		if len(slice.Endpoints) != 1 || len(slice.Endpoints[0].Addresses) != 1 || slice.Endpoints[0].Addresses[0] != net.ParseIP(c.ip).String() {
			t.Errorf("%s: Endpoints = %+v", c.ip, slice.Endpoints)
		}
		if len(slice.Ports) != 1 || *slice.Ports[0].Port != 443 || *slice.Ports[0].Name != OriginPortName {
			t.Errorf("%s: Ports = %+v", c.ip, slice.Ports)
		}
		if slice.Annotations[ValuesHashAnnotation] != hash {
			t.Errorf("%s: hash annotation = %q, want %q", c.ip, slice.Annotations[ValuesHashAnnotation], hash)
		}
	}
}
