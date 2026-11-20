package tenancy

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/miekg/dns"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateSharing(ctx context.Context, reader client.Reader, object, oldObject client.Object) error {
	var sharing *api.ResourceSharing
	switch resource := object.(type) {
	case *api.Zone:
		sharing = resource.Spec.Sharing
	case *api.Location:
		sharing = resource.Spec.Sharing
	default:
		return nil
	}
	if err := api.ValidateResourceSharing(sharing, object.GetNamespace()); err != nil {
		return err
	}
	// Routing consumers must not prevent location pool changes.
	if _, ok := object.(*api.Location); ok {
		return nil
	}
	if oldObject == nil || reflect.DeepEqual(sharingFor(oldObject), sharing) {
		return nil
	}
	services := &api.ServiceList{}
	if err := reader.List(ctx, services); err != nil {
		return fmt.Errorf("list services using shared infrastructure: %w", err)
	}
	for i := range services.Items {
		service := &services.Items[i]
		if !CanUse(service, oldObject) || CanUse(service, object) {
			continue
		}
		if uses, err := serviceUses(service, oldObject); err != nil {
			return err
		} else if uses {
			return fmt.Errorf("cannot revoke sharing while service %s uses this resource; migrate or delete the service first", client.ObjectKeyFromObject(service))
		}
	}
	endpoints := &api.DNSEndpointList{}
	if err := reader.List(ctx, endpoints); err != nil {
		return fmt.Errorf("list DNS endpoints using shared infrastructure: %w", err)
	}
	for i := range endpoints.Items {
		endpoint := &endpoints.Items[i]
		if !CanUse(endpoint, oldObject) || CanUse(endpoint, object) {
			continue
		}
		if uses, err := endpointUses(endpoint, oldObject); err != nil {
			return err
		} else if uses {
			return fmt.Errorf("cannot revoke sharing while DNS endpoint %s uses this resource", client.ObjectKeyFromObject(endpoint))
		}
	}
	return nil
}

func endpointUses(endpoint *api.DNSEndpoint, object client.Object) (bool, error) {
	switch resource := object.(type) {
	case *api.Zone:
		return endpoint.Spec.Zone != nil && endpoint.ZoneKey() == client.ObjectKeyFromObject(resource), nil
	}
	return false, nil
}

func serviceUses(service *api.Service, object client.Object) (bool, error) {
	switch resource := object.(type) {
	case *api.Zone:
		for _, domain := range append([]api.HostAliasSpec{{Name: service.Spec.Domain}}, service.Spec.HostAliases...) {
			if domain.Name != "" && dns.IsSubDomain(dns.Fqdn(resource.Spec.Zone), dns.Fqdn(domain.Name)) {
				return true, nil
			}
		}
	}
	return false, nil
}

func ValidateInfrastructureDeletion(ctx context.Context, reader client.Reader, object client.Object) error {
	switch resource := object.(type) {
	case *api.Zone:
		endpoints := &api.DNSEndpointList{}
		if err := reader.List(ctx, endpoints); err != nil {
			return err
		}
		for i := range endpoints.Items {
			if endpoints.Items[i].Spec.Zone != nil && endpoints.Items[i].ZoneKey() == client.ObjectKeyFromObject(resource) {
				return fmt.Errorf("zone is still in use by DNS endpoint %s", client.ObjectKeyFromObject(&endpoints.Items[i]))
			}
		}
	case *api.Location:
		prefixes := &api.PrefixListList{}
		if err := reader.List(ctx, prefixes); err != nil {
			return err
		}
		for i := range prefixes.Items {
			prefix := &prefixes.Items[i]
			if SameProject(prefix, resource) && prefix.Spec.Destination == resource.Name {
				return fmt.Errorf("location is still referenced by prefix list %s", client.ObjectKeyFromObject(prefix))
			}
		}
		locations := &api.LocationList{}
		if err := reader.List(ctx, locations); err != nil {
			return err
		}
		for i := range locations.Items {
			other := &locations.Items[i]
			if other.Namespace == resource.Namespace && other.Name != resource.Name &&
				(other.Spec.Parent == resource.Name || slices.Contains(other.Spec.FallbackLocations, resource.Name)) {
				return fmt.Errorf("location is still referenced by location %s", client.ObjectKeyFromObject(other))
			}
		}
		return nil
	default:
		return nil
	}
	services := &api.ServiceList{}
	if err := reader.List(ctx, services); err != nil {
		return err
	}
	for i := range services.Items {
		service := &services.Items[i]
		if !CanUse(service, object) {
			continue
		}
		if uses, err := serviceUses(service, object); err != nil {
			return err
		} else if uses {
			return fmt.Errorf("infrastructure is still in use by service %s; migrate or delete it first", client.ObjectKeyFromObject(service))
		}
	}
	endpoints := &api.DNSEndpointList{}
	if err := reader.List(ctx, endpoints); err != nil {
		return err
	}
	for i := range endpoints.Items {
		endpoint := &endpoints.Items[i]
		if !CanUse(endpoint, object) {
			continue
		}
		if uses, err := endpointUses(endpoint, object); err != nil {
			return err
		} else if uses {
			return fmt.Errorf("infrastructure is still in use by DNS endpoint %s", client.ObjectKeyFromObject(endpoint))
		}
	}
	return nil
}
