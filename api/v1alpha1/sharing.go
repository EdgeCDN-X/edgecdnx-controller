package v1alpha1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ProjectReference identifies a consuming project. An omitted namespace means the resource's namespace.
type ProjectReference struct {
	Namespace string `json:"namespace,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ResourceSharing grants consumption, never ownership or management permissions.
type ResourceSharing struct {
	AllProjects bool `json:"allProjects,omitempty"`
	// +listType=atomic
	Projects []ProjectReference `json:"projects,omitempty"`
}

func CanUseResource(sourceNamespace, sourceProject, targetNamespace, targetProject string, sharing *ResourceSharing) bool {
	if sourceNamespace == "" || sourceProject == "" || targetNamespace == "" || targetProject == "" {
		return false
	}
	if sourceNamespace == targetNamespace && sourceProject == targetProject {
		return true
	}
	if sharing == nil {
		return false
	}
	if sharing.AllProjects {
		return true
	}
	for _, project := range sharing.Projects {
		namespace := project.Namespace
		if namespace == "" {
			namespace = targetNamespace
		}
		if namespace == sourceNamespace && project.Name == sourceProject {
			return true
		}
	}
	return false
}

func ValidateResourceSharing(sharing *ResourceSharing, namespace string) error {
	if sharing == nil {
		return nil
	}
	seen := make(map[types.NamespacedName]bool)
	for _, project := range sharing.Projects {
		if problems := validation.IsDNS1123Subdomain(project.Name); len(problems) != 0 {
			return fmt.Errorf("invalid sharing project name %q: %v", project.Name, problems)
		}
		if project.Namespace != "" {
			if problems := validation.IsDNS1123Label(project.Namespace); len(problems) != 0 {
				return fmt.Errorf("invalid sharing project namespace %q: %v", project.Namespace, problems)
			}
		}
		if project.Namespace == "" {
			project.Namespace = namespace
		}
		key := types.NamespacedName{Namespace: project.Namespace, Name: project.Name}
		if seen[key] {
			return fmt.Errorf("duplicate sharing project %s", key)
		}
		seen[key] = true
	}
	return nil
}

// ZoneKey resolves legacy name-only references without mutating persisted objects.
func (endpoint *DNSEndpoint) ZoneKey() types.NamespacedName {
	key := types.NamespacedName{Namespace: endpoint.Namespace}
	if endpoint.Spec.Zone != nil {
		key.Name = endpoint.Spec.Zone.Name
		if endpoint.Spec.Zone.Namespace != "" {
			key.Namespace = endpoint.Spec.Zone.Namespace
		}
	}
	return key
}
