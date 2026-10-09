package builder

import (
	"strings"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

const configSnippetAnnotation = "nginx.ingress.kubernetes.io/configuration-snippet"

func staticOriginService(defaultCacheTTL string) infrastructurev1alpha1.Service {
	return infrastructurev1alpha1.Service{
		Spec: infrastructurev1alpha1.ServiceSpec{
			Domain:          "cdn.example.com",
			Cache:           "ssd",
			OriginType:      infrastructurev1alpha1.OriginTypeStatic,
			DefaultCacheTTL: defaultCacheTTL,
			StaticOrigins: []infrastructurev1alpha1.StaticOriginSpec{
				{Upstream: "origin.example.com", Port: 443, Scheme: "Https", HostHeader: "origin.example.com"},
			},
		},
	}
}

func TestCacheIngressBuilderDefaultCacheTTL(t *testing.T) {
	b := NewCacheIngressBuilder("cdn", "default", CacheIngressBuilderConfig{})
	b.WithService(staticOriginService("10m"))

	ingress, _, err := b.Build()
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	snippet := ingress.Annotations[configSnippetAnnotation]
	if !strings.Contains(snippet, "proxy_cache_valid 200 301 302 10m;") {
		t.Errorf("configuration snippet missing proxy_cache_valid:\n%s", snippet)
	}
}

func TestCacheIngressBuilderNoDefaultCacheTTL(t *testing.T) {
	b := NewCacheIngressBuilder("cdn", "default", CacheIngressBuilderConfig{})
	b.WithService(staticOriginService(""))

	ingress, _, err := b.Build()
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	snippet := ingress.Annotations[configSnippetAnnotation]
	if !strings.Contains(snippet, "proxy_cache ssd;") {
		t.Fatalf("configuration snippet not rendered:\n%s", snippet)
	}
	if strings.Contains(snippet, "proxy_cache_valid") {
		t.Errorf("proxy_cache_valid rendered without defaultCacheTtl:\n%s", snippet)
	}
}
