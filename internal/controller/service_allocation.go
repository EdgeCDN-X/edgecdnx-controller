package controller

import (
	"context"
	"fmt"
	"strings"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/tenancy"
	"github.com/miekg/dns"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const serviceAllocationFinalizer = tenancy.ServiceAllocationFinalizer

func selectServiceZone(service *api.Service, dnsName string, zones []api.Zone) (*api.Zone, error) {
	var selected *api.Zone
	length := 0
	for i := range zones {
		zone := &zones[i]
		if !zone.DeletionTimestamp.IsZero() || !tenancy.CanUse(service, zone) ||
			!dns.IsSubDomain(dns.Fqdn(strings.ToLower(zone.Spec.Zone)), dns.Fqdn(strings.ToLower(dnsName))) {
			continue
		}
		if selected == nil || len(zone.Spec.Zone) > length {
			selected, length = zone, len(zone.Spec.Zone)
		} else if len(zone.Spec.Zone) == length {
			return nil, fmt.Errorf("multiple usable zones cover %q; remove the ambiguous zone", dnsName)
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("no zone available to the service project covers %q", dnsName)
	}
	if !tenancy.SameProject(service, selected) {
		domain := strings.ToLower(dns.Fqdn(dnsName))
		suffix := strings.ToLower(dns.Fqdn(selected.Spec.Zone))
		host := strings.TrimSuffix(domain, "."+suffix)
		if !strings.EqualFold(dns.Fqdn(service.Spec.Domain), domain) ||
			host == domain || len(validation.IsDNS1123Label(host)) != 0 {
			return nil, fmt.Errorf("shared zones only allocate single-label service domains, not aliases or apex records")
		}
	}
	return selected, nil
}

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services/finalizers,verbs=update
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services,verbs=update;patch

func (r *ServiceReconciler) reserveHostname(ctx context.Context, service *api.Service, zone *api.Zone) error {
	if service.UID == "" {
		return fmt.Errorf("cannot allocate a hostname without a service UID")
	}
	if controllerutil.AddFinalizer(service, serviceAllocationFinalizer) {
		if err := r.Update(ctx, service); err != nil {
			return err
		}
	}
	domain := strings.ToLower(dns.Fqdn(service.Spec.Domain))
	identity := string(service.UID)
	allocation := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: zone.Namespace, Name: tenancy.AllocationName(domain),
			Labels:      map[string]string{tenancy.AllocationServiceLabel: identity},
			Annotations: map[string]string{tenancy.AllocationDomainAnnotation: domain},
		},
		Spec: coordinationv1.LeaseSpec{HolderIdentity: &identity},
	}
	if err := controllerutil.SetOwnerReference(zone, allocation, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, allocation); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("reserve service hostname: %w", err)
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(allocation), allocation); err != nil {
			return fmt.Errorf("get hostname allocation: %w", err)
		}
		if allocation.Spec.HolderIdentity == nil || *allocation.Spec.HolderIdentity != identity ||
			allocation.Annotations[tenancy.AllocationDomainAnnotation] != domain {
			return fmt.Errorf("hostname %q is already allocated to another service", domain)
		}
	}
	return nil
}

func (r *ServiceReconciler) releaseHostnames(ctx context.Context, service *api.Service) (ctrl.Result, error) {
	allocations := &coordinationv1.LeaseList{}
	if err := r.List(ctx, allocations, client.MatchingLabels{tenancy.AllocationServiceLabel: string(service.UID)}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range allocations.Items {
		allocation := &allocations.Items[i]
		if allocation.Spec.HolderIdentity == nil || *allocation.Spec.HolderIdentity != string(service.UID) {
			return ctrl.Result{}, fmt.Errorf("hostname allocation %s has a mismatched holder", client.ObjectKeyFromObject(allocation))
		}
		if err := client.IgnoreNotFound(r.Delete(ctx, allocation)); err != nil {
			return ctrl.Result{}, err
		}
	}
	controllerutil.RemoveFinalizer(service, serviceAllocationFinalizer)
	return ctrl.Result{}, r.Update(ctx, service)
}
