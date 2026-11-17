package tenancy

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/miekg/dns"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	if oldObject == nil || reflect.DeepEqual(sharingFor(oldObject), sharing) {
		return nil
	}
	targets := []client.Object{oldObject}
	if location, ok := oldObject.(*api.Location); ok {
		children := &api.LocationList{}
		if err := reader.List(ctx, children, client.InNamespace(location.Namespace)); err != nil {
			return fmt.Errorf("list location hierarchy: %w", err)
		}
		for i := range children.Items {
			child := &children.Items[i]
			if child.Spec.Parent == location.Name && SameProject(child, location) {
				targets = append(targets, child)
			}
		}
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
		for _, target := range targets {
			if uses, err := serviceUses(service, target); err != nil {
				return err
			} else if uses {
				return fmt.Errorf("cannot revoke sharing while service %s uses this resource; migrate or delete the service first", client.ObjectKeyFromObject(service))
			}
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
		for _, target := range targets {
			if uses, err := endpointUses(endpoint, target); err != nil {
				return err
			} else if uses {
				return fmt.Errorf("cannot revoke sharing while DNS endpoint %s uses this resource", client.ObjectKeyFromObject(endpoint))
			}
		}
	}
	return nil
}

func endpointUses(endpoint *api.DNSEndpoint, object client.Object) (bool, error) {
	switch resource := object.(type) {
	case *api.Zone:
		return endpoint.Spec.Zone != nil && endpoint.ZoneKey() == client.ObjectKeyFromObject(resource), nil
	case *api.Location:
		if endpoint.Spec.RoutingPolicy == "Failover" {
			return slices.Contains(endpoint.Spec.Targets, resource.Name) || slices.Contains(endpoint.Spec.Targets, resource.Namespace+"/"+resource.Name), nil
		}
		if endpoint.Spec.RoutingPolicy != "Geolocation" && endpoint.Spec.RoutingPolicy != "Weighted" && endpoint.Spec.RoutingPolicy != "RoundRobin" {
			return false, nil
		}
		source := &api.Service{ObjectMeta: endpoint.ObjectMeta, Spec: api.ServiceSpec{
			Project: endpoint.Spec.Project, RouteSelector: endpoint.Spec.RouteSelector,
		}}
		return serviceUses(source, resource)
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
	case *api.Location:
		selector, err := metav1.LabelSelectorAsSelector(service.Spec.RouteSelector)
		if err != nil {
			return false, fmt.Errorf("invalid selector on service %s: %w", client.ObjectKeyFromObject(service), err)
		}
		// A nil service selector means all usable locations.
		if service.Spec.RouteSelector == nil || selector.Matches(labels.Set(resource.Labels)) {
			return true, nil
		}
		for _, group := range resource.Spec.NodeGroups {
			effective := make(labels.Set, len(resource.Labels)+len(group.Labels))
			for key, value := range resource.Labels {
				effective[key] = value
			}
			for key, value := range group.Labels {
				effective[key] = value
			}
			if selector.Matches(effective) {
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
