package v1alpha1

import (
	"strings"
	"testing"
)

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
