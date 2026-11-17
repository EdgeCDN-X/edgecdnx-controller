package v1alpha1

import (
	"context"
	"strings"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDNSEndpointZoneReference(t *testing.T) {
	tests := []struct {
		name      string
		reference corev1.ObjectReference
		wantError string
	}{
		{name: "legacy", reference: corev1.ObjectReference{Name: "zone"}},
		{name: "explicit local namespace", reference: corev1.ObjectReference{Name: "zone", Namespace: "tenant"}},
		{name: "explicit remote namespace", reference: corev1.ObjectReference{Name: "zone", Namespace: "platform"}},
		{name: "full reference", reference: corev1.ObjectReference{Name: "zone", Namespace: "platform", Kind: "Zone", APIVersion: api.SchemeGroupVersion.String(), UID: "zone-uid"}},
		{name: "missing remote", reference: corev1.ObjectReference{Name: "zone", Namespace: "missing"}, wantError: `does not exist in namespace "missing"`},
		{name: "wrong kind", reference: corev1.ObjectReference{Name: "zone", Kind: "Secret"}, wantError: "spec.zone.kind"},
		{name: "wrong version", reference: corev1.ObjectReference{Name: "zone", APIVersion: "v1"}, wantError: "spec.zone.apiVersion"},
		{name: "stale UID", reference: corev1.ObjectReference{Name: "zone", UID: "old-zone"}, wantError: "spec.zone.uid"},
		{name: "field path", reference: corev1.ObjectReference{Name: "zone", FieldPath: "spec"}, wantError: "fieldPath"},
		{name: "resource version", reference: corev1.ObjectReference{Name: "zone", ResourceVersion: "1"}, wantError: "resourceVersion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := api.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			local := &api.Zone{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "zone", UID: "zone-uid"}, Spec: api.ZoneSpec{Zone: "example.com"}}
			remote := local.DeepCopy()
			remote.Namespace = "platform"
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(local, remote).Build()
			validator := &DNSEndpointCustomValidator{Client: c}
			endpoint := &api.DNSEndpoint{
				ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "record"},
				Spec:       api.DNSEndpointSpec{Zone: &tt.reference, DNSName: "www.example.com", RecordType: "A", Targets: []string{"192.0.2.1"}},
			}
			for _, update := range []bool{false, true} {
				var err error
				if update {
					_, err = validator.ValidateUpdate(context.Background(), endpoint.DeepCopy(), endpoint)
				} else {
					_, err = validator.ValidateCreate(context.Background(), endpoint)
				}
				if tt.wantError == "" && err != nil {
					t.Fatal(err)
				}
				if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
					t.Fatalf("expected %q, got %v", tt.wantError, err)
				}
			}
		})
	}
}

func TestDNSRecordConflictZoneNamespaces(t *testing.T) {
	endpoint := &api.DNSEndpoint{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "record"},
		Spec: api.DNSEndpointSpec{Zone: &corev1.ObjectReference{Name: "zone"}, DNSName: "www.example.com", RecordType: "A"}}
	existing := endpoint.DeepCopy()
	existing.Name = "other"
	existing.Namespace = "another-tenant"
	existing.Spec.Zone.Namespace = "tenant"
	if err := validateDNSRecordConflicts(endpoint, []api.DNSEndpoint{*existing}); err == nil {
		t.Fatal("duplicate records in a shared zone across namespaces accepted")
	}
	existing.Spec.Zone.Namespace = ""
	if err := validateDNSRecordConflicts(endpoint, []api.DNSEndpoint{*existing}); err != nil {
		t.Fatalf("same-named zones in different namespaces were conflated: %v", err)
	}
}
