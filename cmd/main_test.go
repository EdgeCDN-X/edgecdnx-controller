package main

import (
	"reflect"
	"testing"
)

func TestParseTrustedProxyCIDRs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "empty trusts no proxies"},
		{name: "IPv4 and IPv6", input: " 192.0.2.0/24, 2001:db8::/32 ", want: []string{"192.0.2.0/24", "2001:db8::/32"}},
		{name: "invalid CIDR", input: "192.0.2.0/24,invalid", wantErr: true},
		{name: "address without prefix", input: "192.0.2.1", wantErr: true},
		{name: "IPv4-mapped prefix", input: "::ffff:192.0.2.0/120", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTrustedProxyCIDRs(tt.input)
			if (err != nil) != tt.wantErr || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseTrustedProxyCIDRs(%q) = %v, %v; want %v, error=%t", tt.input, got, err, tt.want, tt.wantErr)
			}
		})
	}
}
