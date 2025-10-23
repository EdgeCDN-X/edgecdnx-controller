package builder

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ICoreV1ServiceBuilder interface {
	WithAnnotations(annotations map[string]string)
	WithUpstream(upstream string)
	WithS3Gateway(domain string)
	Build() (v1.Service, string, error)
}

type CoreV1ServiceBuilder struct {
	service v1.Service
}

func (b *CoreV1ServiceBuilder) WithAnnotations(annotations map[string]string) {
	b.service.SetAnnotations(annotations)
}

func (b *CoreV1ServiceBuilder) WithUpstream(upstream string) {
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
