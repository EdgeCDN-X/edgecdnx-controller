package v1alpha1

import (
	"context"
	"strings"
	"testing"

	"github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestServiceCachingReconcilerFeatureGate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	for _, enabled := range []bool{false, true} {
		validator := &ServiceCustomValidator{CachingReconcilerEnabled: enabled}
		if enabled {
			validator.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
		}
		service := &v1alpha1.Service{}
		testCases := []struct {
			name     string
			validate func() (admission.Warnings, error)
			wantErr  bool
		}{
			{
				name: "create",
				validate: func() (admission.Warnings, error) {
					return validator.ValidateCreate(context.Background(), service)
				},
				wantErr: !enabled,
			},
			{
				name: "update",
				validate: func() (admission.Warnings, error) {
					return validator.ValidateUpdate(context.Background(), service, service)
				},
				wantErr: !enabled,
			},
			{
				name: "delete",
				validate: func() (admission.Warnings, error) {
					return validator.ValidateDelete(context.Background(), service)
				},
			},
		}
		gateState := "disabled"
		if enabled {
			gateState = "enabled"
		}
		for _, testCase := range testCases {
			t.Run(gateState+"/"+testCase.name, func(t *testing.T) {
				warnings, err := testCase.validate()
				if len(warnings) != 0 {
					t.Fatalf("expected no warnings, got %v", warnings)
				}
				if testCase.wantErr {
					if err == nil || err.Error() != "Service requires the cachingreconciler feature gate to be enabled" {
						t.Fatalf("expected feature gate rejection, got %v", err)
					}
				} else if err != nil {
					t.Fatalf("expected admission to succeed, got %v", err)
				}
			})
		}
	}

	t.Run("enabled preserves service validation", func(t *testing.T) {
		validator := &ServiceCustomValidator{
			Client:                   fake.NewClientBuilder().WithScheme(scheme).Build(),
			CachingReconcilerEnabled: true,
		}
		service := &v1alpha1.Service{}
		service.Spec.OriginType = "static"
		_, err := validator.ValidateCreate(context.Background(), service)
		if err == nil || err.Error() != "originType is set to static but no staticOrigins are defined" {
			t.Fatalf("expected static origin validation error, got %v", err)
		}
	})
}

func TestValidateUpstreamConfiguration(t *testing.T) {
	testCases := []struct {
		name           string
		upstream       string
		wantErr        bool
		errContainsMsg string
	}{
		{
			name:     "accepts public IPv4",
			upstream: "8.8.8.8",
			wantErr:  false,
		},
		{
			name:           "rejects private IPv4",
			upstream:       "10.20.30.40",
			wantErr:        true,
			errContainsMsg: "private IPv4",
		},
		{
			name:           "rejects non-IPv4 IP",
			upstream:       "2001:db8::1",
			wantErr:        true,
			errContainsMsg: "public IPv4 address or an FQDN",
		},
		{
			name:     "accepts fqdn",
			upstream: "origin.example.com",
			wantErr:  false,
		},
		{
			name:     "accepts fqdn with trailing dot",
			upstream: "origin.example.com.",
			wantErr:  false,
		},
		{
			name:           "rejects non-fqdn",
			upstream:       "localhost",
			wantErr:        true,
			errContainsMsg: "public IPv4 address or an FQDN",
		},
		{
			name:           "rejects blocked local tld",
			upstream:       "origin.edgecdn.local",
			wantErr:        true,
			errContainsMsg: "blocked top-level domain .local",
		},
		{
			name:           "rejects blocked internal tld case insensitive",
			upstream:       "origin.edgecdn.INTERNAL",
			wantErr:        true,
			errContainsMsg: "blocked top-level domain .internal",
		},
		{
			name:           "rejects blocked private tld",
			upstream:       "origin.edgecdn.private",
			wantErr:        true,
			errContainsMsg: "blocked top-level domain .private",
		},
		{
			name:           "rejects invalid label",
			upstream:       "-origin.example.com",
			wantErr:        true,
			errContainsMsg: "public IPv4 address or an FQDN",
		},
		{
			name:           "rejects url format",
			upstream:       "https://origin.example.com",
			wantErr:        true,
			errContainsMsg: "public IPv4 address or an FQDN",
		},
		{
			name:     "accepts trimmed fqdn",
			upstream: "  origin.example.com  ",
			wantErr:  false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			validator := &ServiceCustomValidator{
				BlockedUpstreamTLDs: []string{"local", "internal", "private"},
			}
			err := validator.validateUpstreamConfiguration(testCase.upstream)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected error for upstream %q", testCase.upstream)
				}

				if testCase.errContainsMsg != "" && !strings.Contains(err.Error(), testCase.errContainsMsg) {
					t.Fatalf("expected error containing %q, got %q", testCase.errContainsMsg, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error for upstream %q, got %v", testCase.upstream, err)
			}
		})
	}
}
