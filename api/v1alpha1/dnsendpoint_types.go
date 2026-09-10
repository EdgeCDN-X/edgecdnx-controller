/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DNSEndpointSpec defines the desired state of DNSEndpoint.
type DNSEndpointSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// DNSName is the fully qualified domain name for the DNSEndpoint. Edit dnsendpoint_types.go to remove/update
	DNSName string `json:"dnsName,omitempty"`

	// RoutingPolicy specifies the routing policy for the DNSEndpoint. Edit dnsendpoint_types.go to remove/update
	// +kubebuilder:validation:Enum=Simple;Weighted;Failover;Geolocation;RoundRobin
	RoutingPolicy string `json:"routingPolicy,omitempty"`

	// RecordTTL specifies the time-to-live for the DNS record in seconds. Edit dnsendpoint_types.go to remove/update
	RecordTTL int `json:"recordTTL,omitempty"`

	// RecordType specifies the type of DNS record for the DNSEndpoint. Edit dnsendpoint_types.go to remove/update
	// +kubebuilder:validation:Enum=A;AAAA;CNAME;TXT;MX;SRV;NS
	RecordType string `json:"recordType,omitempty"`

	// Targets specifies the list of target endpoints for the DNSEndpoint. Edit dnsendpoint_types.go to remove/update
	// Used with Simple and Failover routing policies.
	Targets []string `json:"targets,omitempty"`

	// RouteSelector specifies the label selector for routing traffic to the appropriate endpoints. Edit dnsendpoint_types.go to remove/update
	// +kubebuilder:validation:Optional
	// Used with Geolocation, Weighted, RoundRobin routing policies.
	RouteSelector *metav1.LabelSelector `json:"routeSelector,omitempty"`
}

// DNSEndpointStatus defines the observed state of DNSEndpoint.
type DNSEndpointStatus struct {
	// +kubebuilder:validation:Enum=Healthy;Progressing;Degraded
	Status string `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DNSEndpoint is the Schema for the dnsendpoints API.
type DNSEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSEndpointSpec   `json:"spec,omitempty"`
	Status DNSEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DNSEndpointList contains a list of DNSEndpoint.
type DNSEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DNSEndpoint{}, &DNSEndpointList{})
}
