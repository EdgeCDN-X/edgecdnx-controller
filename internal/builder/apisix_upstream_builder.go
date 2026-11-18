package builder

import (
	"strings"

	v1alpha1infrastructure "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IAPISixiUpstreamBuilder interface {
	WithUpstreams(upstream v1alpha1infrastructure.StaticOriginSpec) IAPISixiUpstreamBuilder
	WithClass(class string) IAPISixiUpstreamBuilder
	Build() (apisixv2.ApisixUpstream, string, error)
}

type APISixUpstreamBuilder struct {
	upstream apisixv2.ApisixUpstream
}

// Groups of upstreams sharing the same parameters (scheme and upstream host) are expected.
func (b *APISixUpstreamBuilder) WithUpstreams(upstream v1alpha1infrastructure.StaticOriginSpec) IAPISixiUpstreamBuilder {
	// Only single upstream per upstream block
	// With apisix we can support multiple upstreams tho.
	weight := 1
	b.upstream.Spec.ExternalNodes = []apisixv2.ApisixUpstreamExternalNode{
		{
			Name:   upstream.Upstream,
			Type:   apisixv2.ExternalTypeDomain,
			Weight: &weight,
			Port:   &upstream.Port,
		},
	}
	b.upstream.Spec.Scheme = strings.ToLower(upstream.Scheme)
	b.upstream.Spec.PassHost = "node"

	if upstream.HostHeader != "" {
		b.upstream.Spec.UpstreamHost = upstream.HostHeader
		b.upstream.Spec.PassHost = "rewrite"
	}

	return b
}
func (b *APISixUpstreamBuilder) WithClass(class string) IAPISixiUpstreamBuilder {
	b.upstream.Spec.IngressClassName = class
	return b
}

func (b *APISixUpstreamBuilder) Build() (apisixv2.ApisixUpstream, string, error) {
	return b.upstream, "", nil
}

func NewDefaultAPISixUpstreamBuilder() IAPISixiUpstreamBuilder {
	return &APISixUpstreamBuilder{}
}

func APISixUpstreamBuilderFactory(name string, namespace string) IAPISixiUpstreamBuilder {
	return &APISixUpstreamBuilder{
		upstream: apisixv2.ApisixUpstream{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ApisixUpstream",
				APIVersion: apisixv2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: apisixv2.ApisixUpstreamSpec{},
		},
	}
}
