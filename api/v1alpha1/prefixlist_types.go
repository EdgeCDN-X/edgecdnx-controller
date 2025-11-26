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

type V4PrefixSpec struct {
	// IPv6 Address
	// +kubebuilder:validation:Format=ipv4
	Address string `json:"address"`
	// Subnet size
	// +kubebuilder:validation:Maximum=32
	// +kubebuilder:validation:Minimum=0
	Size int `json:"size"`
}

type V6PrefixSpec struct {
	// IPv4 Address
	// +kubebuilder:validation:Format=ipv6
	Address string `json:"address"`
	// Subnet size
	// +kubebuilder:validation:Maximum=128
	// +kubebuilder:validation:Minimum=0
	Size int `json:"size"`
}

type PrefixSpec struct {
	// IPv4 Prefixes
	V4 []V4PrefixSpec `json:"v4,omitempty"`
	// IPv6 Prefixes
	V6 []V6PrefixSpec `json:"v6,omitempty"`
}

// PrefixListSpec defines the desired state of PrefixList.
type PrefixListSpec struct {
	// Source is the source of the prefix list. Either static of Bgp
	// +kubebuilder:validation:Enum=Static;Bgp;Controller
	Source string `json:"source"`
	// Prefixes defined for this PrefixList
	Prefix PrefixSpec `json:"prefix"`
	// Destination Location where this prefix list is routing to
	// +kubebuilder:validation:Required
	Destination string `json:"destination"`
}

// PrefixListStatus defines the observed state of PrefixList.
type PrefixListStatus struct {
	// +kubebuilder:validation:Enum=Healthy;Progressing;Degraded
	Status string `json:"status,omitempty"`
	// +kubebuilder:validation:Enum=Consolidating;Consolidated;Requested
	ConsoliadtionStatus string `json:"consolidationStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:object:generate=true

// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.source"
// +kubebuilder:printcolumn:name="Destination",type="string",JSONPath=".spec.destination"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status"
// +kubebuilder:printcolumn:name="Consolidation Status",type="string",JSONPath=".status.consolidationStatus"
// PrefixList is the Schema for the prefixlists API.
type PrefixList struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PrefixListSpec   `json:"spec,omitempty"`
	Status PrefixListStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// PrefixListList contains a list of PrefixList.
type PrefixListList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrefixList `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PrefixList{}, &PrefixListList{})
}
