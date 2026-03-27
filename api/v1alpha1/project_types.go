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

type RuleSpec struct {
	// Rule type: p, p2, g, g2, ...
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(p|g)\d*$`
	PType string `json:"ptype"`

	// Positional parameters v0
	// +kubebuilder:selectablefield:JSONPath=.spec.v0
	V0 string `json:"v0,omitempty"`

	// Positional parameters v1
	// +kubebuilder:validation:Pattern=`.*\S.*`
	// +kubebuilder:selectablefield:JSONPath=.spec.v1
	V1 string `json:"v1,omitempty"`

	// Positional parameters v2
	// +kubebuilder:selectablefield:JSONPath=.spec.v2
	V2 string `json:"v2,omitempty"`

	// Positional parameters v3
	// +kubebuilder:selectablefield:JSONPath=.spec.v3
	V3 string `json:"v3,omitempty"`

	// Positional parameters v4
	// +kubebuilder:selectablefield:JSONPath=.spec.v4
	V4 string `json:"v4,omitempty"`

	// Positional parameters v5
	// +kubebuilder:selectablefield:JSONPath=.spec.v5
	V5 string `json:"v5,omitempty"`
}

type RBACSpec struct {
	Groups []RuleSpec `json:"groups,omitempty"`
	Rules  []RuleSpec `json:"rules,omitempty"`
}

// ProjectSpec defines the desired state of Project.
type ProjectSpec struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Rbac        RBACSpec `json:"rbac,omitempty"`
}

// ProjectStatus defines the observed state of Project.
type ProjectStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Description",type="string",JSONPath=".spec.description"
// Project is the Schema for the projects API.
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectSpec   `json:"spec,omitempty"`
	Status ProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProjectList contains a list of Project.
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
