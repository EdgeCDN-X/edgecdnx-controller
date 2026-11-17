package v1alpha1

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestServiceHostAliasTargetType(t *testing.T) {
	tests := []struct {
		name, targetType, domain, zoneNamespace, zoneProject, zoneDomain string
		deleting, failList                                               bool
		wantError                                                        string
	}{
		{name: "owned subdomain", targetType: "DELEGATION", domain: "www.example.com", zoneNamespace: "tenant", zoneProject: "customer", zoneDomain: "example.com"},
		{name: "owned apex", targetType: "DELEGATION", domain: "example.com", zoneNamespace: "tenant", zoneProject: "customer", zoneDomain: "example.com"},
		{name: "canonical DNS names", targetType: "DELEGATION", domain: "WWW.Example.COM.", zoneNamespace: "tenant", zoneProject: "customer", zoneDomain: "Example.COM."},
		{name: "no zone", targetType: "DELEGATION", domain: "www.example.com", wantError: "zone in the service project"},
		{name: "other project even shared", targetType: "DELEGATION", domain: "www.example.com", zoneNamespace: "tenant", zoneProject: "platform", zoneDomain: "example.com", wantError: "zone in the service project"},
		{name: "other namespace same project", targetType: "DELEGATION", domain: "www.example.com", zoneNamespace: "other", zoneProject: "customer", zoneDomain: "example.com", wantError: "zone in the service project"},
		{name: "suffix boundary", targetType: "DELEGATION", domain: "notexample.com", zoneNamespace: "tenant", zoneProject: "customer", zoneDomain: "example.com", wantError: "zone in the service project"},
		{name: "deleting zone", targetType: "DELEGATION", domain: "www.example.com", zoneNamespace: "tenant", zoneProject: "customer", zoneDomain: "example.com", deleting: true, wantError: "zone in the service project"},
		{name: "wildcard", targetType: "DELEGATION", domain: "*.example.com", wantError: "valid DNS name"},
		{name: "unsupported type", targetType: "ZONE", domain: "www.example.com", wantError: "targetType"},
		{name: "external CNAME", targetType: "CNAME", domain: "www.external.com"},
		{name: "legacy untyped alias", domain: "www.external.com"},
		{name: "zone list failure", targetType: "DELEGATION", domain: "www.example.com", failList: true, wantError: "failed to list zones"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := api.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.zoneDomain != "" {
				zone := &api.Zone{ObjectMeta: metav1.ObjectMeta{Name: "zone", Namespace: tt.zoneNamespace},
					Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: tt.zoneProject}, Zone: tt.zoneDomain,
						Sharing: &api.ResourceSharing{AllProjects: true}}}
				if tt.deleting {
					now := metav1.Now()
					zone.DeletionTimestamp, zone.Finalizers = &now, []string{"test/cleanup"}
				}
				builder.WithObjects(zone)
			}
			if tt.failList {
				builder.WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return errors.New("list unavailable")
				}})
			}
			validator := &ServiceCustomValidator{Client: builder.Build(), CachingReconcilerEnabled: true}
			service := &api.Service{ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: "tenant"},
				Spec: api.ServiceSpec{Project: &corev1.LocalObjectReference{Name: "customer"}, Domain: "service.cdn.test",
					HostAliases: []api.HostAliasSpec{{Name: tt.domain, TargetType: tt.targetType}}}}
			for _, update := range []bool{false, true} {
				var err error
				if update {
					_, err = validator.ValidateUpdate(context.Background(), service.DeepCopy(), service)
				} else {
					_, err = validator.ValidateCreate(context.Background(), service)
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
