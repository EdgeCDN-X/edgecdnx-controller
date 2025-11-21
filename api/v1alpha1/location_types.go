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

type GeoLookupAttributeValuesSpec struct {
	// Value is the value of the attribute.
	Value string `json:"value,omitempty"`
	// Additional modifier Weight for the value.
	Weight int `json:"weight,omitempty"`
}

type GeoLookupAttributeSpec struct {
	// Weight of this attribute
	Weight int `json:"weight,omitempty"`
	// Attribute Values.
	Values []GeoLookupAttributeValuesSpec `json:"values,omitempty"`
}

type GeoLookupSpec struct {
	// Weight of this location in case of load balancing between multiple locations.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Weight int `json:"weight,omitempty"`
	// Attributes assigned for this location for GeoLookup.
	Attributes map[string]GeoLookupAttributeSpec `json:"attributes,omitempty"`
}

type NodeSpec struct {
	// Name is the name of the node.
	Name string `json:"name"`
	// Ipv4 is the IPv4 address of the node.
	Ipv4 string `json:"ipv4,omitempty"`
	// Ipv6 is the IPv6 address of the node.
	Ipv6 string `json:"ipv6,omitempty"`
	// Caches is the list of caches that are associated with this node.
	// +listType=set
	Caches []string `json:"caches,omitempty"`
	// MaintenanceMode indicates if the node is in maintenance mode.
	MaintenanceMode bool `json:"maintenanceMode,omitempty"`
}

type CacheConfigSpec struct {
	// Name is the name of the cache.
	// Deprecated. Cache Name corresponds to nodeGroup Name
	Name string `json:"name,omitempty"`
	// Path is the path to the cache.
	Path string `json:"path"`
	// KeysZone is the zone for the cache keys.
	KeysZone string `json:"keysZone"`
	// Inactive is the inactive time for the cache.
	Inactive string `json:"inactive"`
	// MaxSize is the maximum size for the cache.
	MaxSize string `json:"maxSize"`
}

type NodeGroupSpec struct {
	// Name is the name of the node group.
	Name string `json:"name"`
	// Nodes is the list of nodes that are part of this node group. If used in this context Caches are ignored in NodeSpec.
	// +listType=map
	// +listMapKey=name
	Nodes []NodeSpec `json:"nodes,omitempty"`
	// CacheConfig is the cache configuration for this node group.
	CacheConfig CacheConfigSpec `json:"cacheConfig"`

	// NodeSelector to apply to the daemonset
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// LocationSpec defines the desired state of Location.
// +kubebuilder:validation:ExactlyOneOf=ndoes,nodeGroups
type LocationSpec struct {

	// Specifies the list of locations that this location can fall back to.
	// +listType=set
	FallbackLocations []string `json:"fallbackLocations,omitempty"`
	// Specifies the list of nodes that are part of this location.
	Nodes []NodeSpec `json:"nodes,omitempty"`
	// Specifies the geo lookup configuration for this location.
	GeoLookup GeoLookupSpec `json:"geoLookup"`
	// Sets the Location to Maintenance Mode.
	MaintenanceMode bool `json:"maintenanceMode,omitempty"`
	// Introduces NodeGroups for location. Either Nodes or NodeGroups can be used.
	// +listType=map
	// +listMapKey=name
	NodeGroups []NodeGroupSpec `json:"nodeGroups,omitempty"`
}

type NodeConditionType string

const (
	IPV4HealthCheckSuccessful NodeConditionType = "IPV4HealthCheckSuccessful"
	IPV6HealthCheckSuccessful NodeConditionType = "IPV6HealthCheckSuccessful"
)

type NodeCondition struct {
	// Condition Type
	Type NodeConditionType `json:"type"`
	// Status
	Status             bool        `json:"status"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	Reason             string      `json:"reason,omitempty"`
	// Observed generation corresponds to the Location generation when the condition was last updated.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type NodeInstanceStatus struct {
	Conditions []NodeCondition `json:"conditions"`
}

// LocationStatus defines the observed state of Location.
type LocationStatus struct {
	// +kubebuilder:validation:Enum=Healthy;Progressing;Degraded
	Status     string                        `json:"status,omitempty"`
	NodeStatus map[string]NodeInstanceStatus `json:"nodeStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Location is the Schema for the locations API.
type Location struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LocationSpec   `json:"spec,omitempty"`
	Status LocationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LocationList contains a list of Location.
type LocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Location `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Location{}, &LocationList{})
}
