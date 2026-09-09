package builder

import (
	"crypto/md5"
	"encoding/json"
	"fmt"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DNSRoutingPolicyGeolocation = "Geolocation"
	DefaultDNSRecordTTL         = 10
)

type IDNSEndpointBuilder interface {
	WithDNSName(dnsName string)
	WithRecordType(recordType string)
	WithRouteSelector(routeSelector *metav1.LabelSelector)
	WithService(service infrastructurev1alpha1.Service, dnsName string, recordType string)
	Build() (infrastructurev1alpha1.DNSEndpoint, string, error)
}

type DefaultDNSEndpointBuilder struct {
	dnsEndpoint infrastructurev1alpha1.DNSEndpoint
}

func NewDefaultDNSEndpointBuilder(name string, namespace string) *DefaultDNSEndpointBuilder {
	return &DefaultDNSEndpointBuilder{
		dnsEndpoint: infrastructurev1alpha1.DNSEndpoint{
			TypeMeta: metav1.TypeMeta{
				APIVersion: infrastructurev1alpha1.SchemeGroupVersion.String(),
				Kind:       "DNSEndpoint",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: infrastructurev1alpha1.DNSEndpointSpec{
				RoutingPolicy: DNSRoutingPolicyGeolocation,
				RecordTTL:     DefaultDNSRecordTTL,
				Targets:       nil,
			},
		},
	}
}

func (b *DefaultDNSEndpointBuilder) WithDNSName(dnsName string) {
	b.dnsEndpoint.Spec.DNSName = dnsName
}

func (b *DefaultDNSEndpointBuilder) WithRecordType(recordType string) {
	b.dnsEndpoint.Spec.RecordType = recordType
}

func (b *DefaultDNSEndpointBuilder) WithRouteSelector(routeSelector *metav1.LabelSelector) {
	if routeSelector == nil {
		b.dnsEndpoint.Spec.RouteSelector = nil
		return
	}

	b.dnsEndpoint.Spec.RouteSelector = routeSelector.DeepCopy()
}

func (b *DefaultDNSEndpointBuilder) WithService(service infrastructurev1alpha1.Service, dnsName string, recordType string) {
	b.WithDNSName(dnsName)
	b.WithRecordType(recordType)
	b.WithRouteSelector(service.Spec.RouteSelector)
}

func (b *DefaultDNSEndpointBuilder) Build() (infrastructurev1alpha1.DNSEndpoint, string, error) {
	marshalled, err := json.Marshal(b.dnsEndpoint)
	if err != nil {
		return infrastructurev1alpha1.DNSEndpoint{}, "", err
	}

	hash := fmt.Sprintf("%x", md5.Sum(marshalled))
	b.dnsEndpoint.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("dnsendpoint-builder")
	logger.V(1).Info("Built DNSEndpoint", "name", b.dnsEndpoint.Name, "namespace", b.dnsEndpoint.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.dnsEndpoint, hash, nil
}

func DNSEndpointBuilderFactory(builderType string, name string, namespace string) (IDNSEndpointBuilder, error) {
	switch builderType {
	case "Service":
		return NewDefaultDNSEndpointBuilder(name, namespace), nil
	default:
		return nil, fmt.Errorf("unknown builder type: %s", builderType)
	}
}
