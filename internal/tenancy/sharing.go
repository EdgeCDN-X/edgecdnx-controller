package tenancy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/miekg/dns"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AllocationServiceLabel     = "edgecdnx.com/allocation-service"
	AllocationDomainAnnotation = "edgecdnx.com/allocation-domain"
	ServiceAllocationFinalizer = "edgecdnx.com/hostname-allocation"
)

func CanUse(source, target client.Object) bool {
	sourceProject, targetProject := ProjectReference(source), ProjectReference(target)
	if sourceProject == nil || targetProject == nil {
		return false
	}
	return api.CanUseResource(source.GetNamespace(), sourceProject.Name, target.GetNamespace(), targetProject.Name, sharingFor(target))
}

func sharingFor(target client.Object) *api.ResourceSharing {
	switch resource := target.(type) {
	case *api.Zone:
		return resource.Spec.Sharing
	case *api.Location:
		return resource.Spec.Sharing
	}
	return nil
}

func AllocationName(domain string) string {
	return fmt.Sprintf("dnsalloc-%x", sha256.Sum256([]byte(strings.ToLower(dns.Fqdn(domain)))))
}

func ValidateZoneReference(reference *api.DNSEndpoint) error {
	zone := reference.Spec.Zone
	if zone == nil || zone.Name == "" {
		return fmt.Errorf("spec.zone is required")
	}
	if zone.Kind != "" && zone.Kind != "Zone" {
		return fmt.Errorf("spec.zone.kind must be Zone")
	}
	if zone.APIVersion != "" && zone.APIVersion != api.SchemeGroupVersion.String() {
		return fmt.Errorf("spec.zone.apiVersion must be %s", api.SchemeGroupVersion.String())
	}
	if zone.FieldPath != "" || zone.ResourceVersion != "" {
		return fmt.Errorf("spec.zone.fieldPath and resourceVersion are not supported")
	}
	return nil
}

func ValidateZoneUse(ctx context.Context, reader client.Reader, endpoint *api.DNSEndpoint, zone *api.Zone) error {
	if !zone.DeletionTimestamp.IsZero() {
		return fmt.Errorf("zone %s is being deleted", client.ObjectKeyFromObject(zone))
	}
	if endpoint.Spec.Zone.UID != "" && endpoint.Spec.Zone.UID != zone.UID {
		return fmt.Errorf("spec.zone.uid does not match the referenced Zone")
	}
	if SameProject(endpoint, zone) {
		return nil
	}
	if !CanUse(endpoint, zone) {
		return fmt.Errorf("zone %s is not shared with the endpoint project", client.ObjectKeyFromObject(zone))
	}
	owner := metav1.GetControllerOf(endpoint)
	if owner == nil || owner.Kind != "Service" || owner.APIVersion != api.SchemeGroupVersion.String() || owner.UID == "" {
		return fmt.Errorf("shared zones only accept service-managed DNS endpoints")
	}
	service := &api.Service{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: endpoint.Namespace, Name: owner.Name}, service); err != nil {
		return fmt.Errorf("resolve endpoint service: %w", err)
	}
	if service.UID != owner.UID || !SameProject(endpoint, service) || !service.DeletionTimestamp.IsZero() {
		return fmt.Errorf("endpoint owner must be a live Service in the endpoint project")
	}
	domain := strings.ToLower(dns.Fqdn(endpoint.Spec.DNSName))
	suffix := strings.ToLower(dns.Fqdn(zone.Spec.Zone))
	host := strings.TrimSuffix(domain, "."+suffix)
	if domain != strings.ToLower(dns.Fqdn(service.Spec.Domain)) || host == domain || len(validation.IsDNS1123Label(host)) != 0 {
		return fmt.Errorf("shared zone endpoints must use the service's allocated single-label hostname")
	}
	if (endpoint.Spec.RecordType != "A" && endpoint.Spec.RecordType != "AAAA") ||
		endpoint.Spec.RoutingPolicy != "Geolocation" || len(endpoint.Spec.Targets) != 0 {
		return fmt.Errorf("shared zone endpoints must use service-managed Geolocation A or AAAA routing")
	}
	if !reflect.DeepEqual(endpoint.Spec.RouteSelector, service.Spec.RouteSelector) {
		return fmt.Errorf("shared zone endpoint routing must match its service")
	}
	allocation := &coordinationv1.Lease{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: zone.Namespace, Name: AllocationName(domain)}, allocation); err != nil {
		return fmt.Errorf("resolve hostname allocation: %w", err)
	}
	if allocation.Spec.HolderIdentity == nil || *allocation.Spec.HolderIdentity != string(service.UID) ||
		allocation.Annotations[AllocationDomainAnnotation] != domain {
		return fmt.Errorf("hostname is not allocated to the endpoint service")
	}
	return nil
}
