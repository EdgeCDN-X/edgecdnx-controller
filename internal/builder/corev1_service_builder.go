package builder

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ICoreV1ServiceBuilder interface {
	WithAnnotations(annotations map[string]string)
	WithUpstream(upstream string, port int)
	WithS3Gateway(domain string)
	Build() (v1.Service, string, error)
}

type CoreV1ServiceBuilder struct {
	service v1.Service
}

func (b *CoreV1ServiceBuilder) WithAnnotations(annotations map[string]string) {
	b.service.SetAnnotations(annotations)
}

// WithUpstream points the Service at a static origin. An FQDN becomes an
// ExternalName Service. An IP address cannot be used as an ExternalName (it is
// a DNS name by definition, and ingress-nginx fails to resolve it), so an IP
// becomes a headless, selector-less Service whose endpoints are provided by an
// EndpointSlice (see EndpointSliceBuilder.WithIPUpstream).
func (b *CoreV1ServiceBuilder) WithUpstream(upstream string, port int) {
	if net.ParseIP(upstream) != nil {
		b.service.Spec = v1.ServiceSpec{
			Type:      v1.ServiceTypeClusterIP,
			ClusterIP: v1.ClusterIPNone,
			Ports: []v1.ServicePort{
				{
					Name:       OriginPortName,
					Port:       int32(port),
					Protocol:   v1.ProtocolTCP,
					TargetPort: intstr.FromInt(port),
				},
			},
		}
		return
	}

	b.service.Spec = v1.ServiceSpec{
		ExternalName: upstream,
		Type:         v1.ServiceTypeExternalName,
	}
}

func (b *CoreV1ServiceBuilder) WithS3Gateway(domain string) {
	b.service.Spec = v1.ServiceSpec{
		ClusterIP: "None",
		Ports: []v1.ServicePort{
			{
				Name:       "http",
				Port:       80,
				Protocol:   v1.ProtocolTCP,
				TargetPort: intstr.FromInt(80),
			},
		},
		Selector: map[string]string{
			"app":    "s3gateway",
			"domain": domain,
		},
	}
}

func (b *CoreV1ServiceBuilder) Build() (v1.Service, string, error) {
	marshalled, err := json.Marshal(b.service)
	if err != nil {
		return v1.Service{}, "", err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(marshalled))

	b.service.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("service-builder")
	logger.V(1).Info("Built Service", "name", b.service.Name, "namespace", b.service.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.service, hash, nil
}

func NewDefaultCoreV1ServiceBuilder(name string, namespace string) *CoreV1ServiceBuilder {
	return &CoreV1ServiceBuilder{
		service: v1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v1.SchemeGroupVersion.String(),
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: v1.ServiceSpec{},
		},
	}
}

func CoreV1ServiceBuilderFactory(builderType string, name string, namespace string) (ICoreV1ServiceBuilder, error) {
	switch builderType {
	case "default":
		b := NewDefaultCoreV1ServiceBuilder(name, namespace)
		return b, nil
	default:
		return nil, fmt.Errorf("unknown builder type: %s", builderType)
	}
}
