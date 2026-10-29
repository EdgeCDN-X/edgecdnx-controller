/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

// Package v1alpha1 contains API Schema definitions for the infrastructure v1alpha1 API group.
// +kubebuilder:object:generate=true
// +kubebuilder:ac:generate=true
// +kubebuilder:object:root=true
// +groupName=infrastructure.edgecdnx.com
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	SchemeGroupVersion = schema.GroupVersion{Group: "infrastructure.edgecdnx.com", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
