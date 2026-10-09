package builder

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// EndpointSliceManagedBy is the value of the endpointslice.kubernetes.io/managed-by
// label on slices created by this controller, so the EndpointSlice controller
// leaves them alone.
const EndpointSliceManagedBy = "edgecdnx-controller"

// OriginPortName is the port name shared by the origin Service and its EndpointSlice.
const OriginPortName = "origin"

type EndpointSliceBuilder struct {
	endpointSlice discoveryv1.EndpointSlice
}

// WithIPUpstream points the slice at a single IP upstream. The slice is linked to
// serviceName via the kubernetes.io/service-name label.
func (b *EndpointSliceBuilder) WithIPUpstream(serviceName string, ip net.IP, port int) {
	addressType := discoveryv1.AddressTypeIPv4
	if ip.To4() == nil {
		addressType = discoveryv1.AddressTypeIPv6
	}

	ready := true
	portName := OriginPortName
	portNumber := int32(port)
	protocol := v1.ProtocolTCP

	b.endpointSlice.Labels = map[string]string{
		discoveryv1.LabelServiceName: serviceName,
		discoveryv1.LabelManagedBy:   EndpointSliceManagedBy,
	}
	b.endpointSlice.AddressType = addressType
	b.endpointSlice.Endpoints = []discoveryv1.Endpoint{
		{
			Addresses:  []string{ip.String()},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
		},
	}
	b.endpointSlice.Ports = []discoveryv1.EndpointPort{
		{
			Name:     &portName,
			Port:     &portNumber,
			Protocol: &protocol,
		},
	}
}

func (b *EndpointSliceBuilder) Build() (discoveryv1.EndpointSlice, string, error) {
	marshalled, err := json.Marshal(b.endpointSlice)
	if err != nil {
		return discoveryv1.EndpointSlice{}, "", err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(marshalled))

	b.endpointSlice.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("endpointslice-builder")
	logger.V(1).Info("Built EndpointSlice", "name", b.endpointSlice.Name, "namespace", b.endpointSlice.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.endpointSlice, hash, nil
}

func NewEndpointSliceBuilder(name string, namespace string) *EndpointSliceBuilder {
	return &EndpointSliceBuilder{
		endpointSlice: discoveryv1.EndpointSlice{
			TypeMeta: metav1.TypeMeta{
				APIVersion: discoveryv1.SchemeGroupVersion.String(),
				Kind:       "EndpointSlice",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}
