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
	Value  string `json:"value,omitempty"`
	Weight int    `json:"weight,omitempty"`
}

type GeoLookupAttributeSpec struct {
	Weight int                            `json:"weight,omitempty"`
	Values []GeoLookupAttributeValuesSpec `json:"values,omitempty"`
}

type GeoLookupSpec struct {
	Weight     int                               `json:"weight,omitempty"`
	Attributes map[string]GeoLookupAttributeSpec `json:"attributes,omitempty"`
}

type NodeSpec struct {
	Name   string   `json:"name,omitempty"`
	Ipv4   string   `json:"ipv4,omitempty"`
	Ipv6   string   `json:"ipv6,omitempty"`
	Caches []string `json:"caches,omitempty"`
}

// LocationSpec defines the desired state of Location.
type LocationSpec struct {
	FallbackLocations []string   `json:"fallbackLocations,omitempty"`
	Nodes             []NodeSpec `json:"nodes,omitempty"`
}

// LocationStatus defines the observed state of Location.
type LocationStatus struct{}

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
