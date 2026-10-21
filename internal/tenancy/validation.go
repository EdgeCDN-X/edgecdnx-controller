package tenancy

import (
	"context"
	"fmt"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ProjectReference(object client.Object) *corev1.LocalObjectReference {
	switch resource := object.(type) {
	case *api.APIKey:
		return resource.Spec.Project
	case *api.DNSEndpoint:
		return resource.Spec.Project
	case *api.HealthCheckProfile:
		return resource.Spec.Project
	case *api.Location:
		return resource.Spec.Project
	case *api.PrefixList:
		return resource.Spec.Project
	case *api.Service:
		return resource.Spec.Project
	case *api.Zone:
		return resource.Spec.Project
	}
	return nil
}

func SameProject(first, second client.Object) bool {
	firstRef, secondRef := ProjectReference(first), ProjectReference(second)
	return first.GetNamespace() == second.GetNamespace() && firstRef != nil && secondRef != nil && firstRef.Name == secondRef.Name
}

func (s Scope) ValidateResource(ctx context.Context, reader client.Reader, object, oldObject client.Object) error {
	if project, ok := object.(*api.Project); ok {
		return s.ValidateProject(project.Namespace, project.Name)
	}
	if err := s.ValidateNamespace(object.GetNamespace()); err != nil {
		return err
	}
	reference := ProjectReference(object)
	if reference == nil || reference.Name == "" {
		return fmt.Errorf("spec.project.name is required")
	}
	if err := s.ValidateProject(object.GetNamespace(), reference.Name); err != nil {
		return err
	}
	if oldObject != nil {
		oldReference := ProjectReference(oldObject)
		if oldReference == nil || oldReference.Name != reference.Name {
			return fmt.Errorf("spec.project is immutable")
		}
	}
	project := &api.Project{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: reference.Name}, project); err != nil {
		return fmt.Errorf("resolve spec.project: %w", err)
	}
	if !project.DeletionTimestamp.IsZero() {
		return fmt.Errorf("project %q is being deleted", project.Name)
	}
	validateReference := func(name string, target client.Object) error {
		if name == "" {
			return fmt.Errorf("referenced resource name is required")
		}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: name}, target); err != nil {
			return fmt.Errorf("resolve reference %q: %w", name, err)
		}
		if !SameProject(object, target) {
			return fmt.Errorf("reference %q must belong to project %q", name, reference.Name)
		}
		return nil
	}
	validateHealthCheck := func(reference *corev1.LocalObjectReference) error {
		if reference == nil {
			return nil
		}
		return validateReference(reference.Name, &api.HealthCheckProfile{})
	}
	switch resource := object.(type) {
	case *api.DNSEndpoint:
		if resource.Spec.Zone != nil {
			return validateReference(resource.Spec.Zone.Name, &api.Zone{})
		}
	case *api.PrefixList:
		return validateReference(resource.Spec.Destination, &api.Location{})
	case *api.Location:
		locations := append([]string{}, resource.Spec.FallbackLocations...)
		if resource.Spec.Parent != "" {
			locations = append(locations, resource.Spec.Parent)
		}
		for _, name := range locations {
			if name == resource.Name {
				return fmt.Errorf("location cannot reference itself")
			}
			if err := validateReference(name, &api.Location{}); err != nil {
				return err
			}
		}
		nodes := append([]api.NodeSpec{}, resource.Spec.Nodes...)
		for _, group := range resource.Spec.NodeGroups {
			if err := validateHealthCheck(group.HealthCheck); err != nil {
				return err
			}
			nodes = append(nodes, group.Nodes...)
		}
		for _, node := range nodes {
			if err := validateHealthCheck(node.HealthCheck); err != nil {
				return err
			}
		}
	}
	return nil
}
