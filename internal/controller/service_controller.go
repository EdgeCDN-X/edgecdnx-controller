/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package controller

import (
	"context"
	"crypto/md5"
	"fmt"
	"reflect"
	"strings"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/tenancy"
	"github.com/miekg/dns"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// ServiceReconciler manages service DNS endpoints on the control plane.
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=zones,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=dnsendpoints,verbs=get;list;watch;create;update;patch;delete

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	service := &infrastructurev1alpha1.Service{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !service.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(service, serviceAllocationFinalizer) {
			return r.releaseHostnames(ctx, service)
		}
		return ctrl.Result{}, nil
	}

	desiredNames := make(map[string]struct{})
	zones := &infrastructurev1alpha1.ZoneList{}
	if err := r.List(ctx, zones); err != nil {
		return ctrl.Result{}, err
	}
	ownedZones := make([]infrastructurev1alpha1.Zone, 0, len(zones.Items))
	for _, zone := range zones.Items {
		if tenancy.SameProject(service, &zone) {
			ownedZones = append(ownedZones, zone)
		}
	}
	dnsNames := []string{service.Spec.Domain}
	for _, hostAlias := range service.Spec.HostAliases {
		switch hostAlias.TargetType {
		case "CNAME":
			continue
		case "", "DELEGATION":
			hasOwnedZone := false
			for _, zone := range ownedZones {
				if zone.DeletionTimestamp.IsZero() && dns.IsSubDomain(dns.Fqdn(strings.ToLower(zone.Spec.Zone)), dns.Fqdn(strings.ToLower(hostAlias.Name))) {
					hasOwnedZone = true
					break
				}
			}
			if !hasOwnedZone {
				if hostAlias.TargetType == "DELEGATION" {
					return ctrl.Result{}, fmt.Errorf("DELEGATION host alias %q has no matching zone in the service project", hostAlias.Name)
				}
				log.Info("Skipping unmanaged legacy host alias without an owned zone", "hostAlias", hostAlias.Name)
				continue
			}
		default:
			return ctrl.Result{}, fmt.Errorf("unsupported host alias targetType %q", hostAlias.TargetType)
		}
		dnsNames = append(dnsNames, hostAlias.Name)
	}

	for i, dnsName := range dnsNames {
		candidateZones := zones.Items
		if i > 0 {
			candidateZones = ownedZones
		}
		zone, err := selectServiceZone(service, dnsName, candidateZones)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !tenancy.SameProject(service, zone) {
			if err := r.reserveHostname(ctx, service, zone); err != nil {
				return ctrl.Result{}, err
			}
		}
		for _, recordType := range []string{"A", "AAAA"} {
			name := serviceDNSEndpointName(service.Name, dnsName, recordType)
			desiredNames[name] = struct{}{}

			dnsEndpointBuilder, err := builder.DNSEndpointBuilderFactory("Service", name, service.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}
			dnsEndpointBuilder.WithService(*service, dnsName, recordType)
			desiredDNSEndpoint, hash, err := dnsEndpointBuilder.Build()
			if err != nil {
				return ctrl.Result{}, err
			}
			desiredDNSEndpoint.Spec.Zone = &corev1.ObjectReference{Name: zone.Name, Namespace: zone.Namespace}

			currentDNSEndpoint := &infrastructurev1alpha1.DNSEndpoint{}
			key := types.NamespacedName{Name: name, Namespace: service.Namespace}
			if err := r.Get(ctx, key, currentDNSEndpoint); err != nil {
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}

				if err := controllerutil.SetControllerReference(service, &desiredDNSEndpoint, r.Scheme); err != nil {
					return ctrl.Result{}, err
				}
				log.Info("Creating DNSEndpoint for Service", "dnsEndpoint", key)
				if err := r.Create(ctx, &desiredDNSEndpoint); err != nil {
					return ctrl.Result{}, err
				}
				continue
			}

			if !tenancy.SameProject(service, currentDNSEndpoint) {
				return ctrl.Result{}, fmt.Errorf("DNSEndpoint %s belongs to another project", key)
			}
			currentHash, hasHash := currentDNSEndpoint.Annotations[builder.ValuesHashAnnotation]
			if hasHash && currentHash == hash && metav1.IsControlledBy(currentDNSEndpoint, service) && reflect.DeepEqual(currentDNSEndpoint.Spec, desiredDNSEndpoint.Spec) {
				continue
			}

			currentDNSEndpoint.Spec = desiredDNSEndpoint.Spec
			currentDNSEndpoint.Annotations = desiredDNSEndpoint.Annotations
			if err := controllerutil.SetControllerReference(service, currentDNSEndpoint, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Updating DNSEndpoint for Service", "dnsEndpoint", key, "oldHash", currentHash, "newHash", hash)
			if err := r.Update(ctx, currentDNSEndpoint); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	dnsEndpoints := &infrastructurev1alpha1.DNSEndpointList{}
	if err := r.List(ctx, dnsEndpoints, client.InNamespace(service.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	for i := range dnsEndpoints.Items {
		dnsEndpoint := &dnsEndpoints.Items[i]
		if !metav1.IsControlledBy(dnsEndpoint, service) {
			continue
		}
		if _, desired := desiredNames[dnsEndpoint.Name]; desired {
			continue
		}

		log.Info("Deleting stale DNSEndpoint for Service", "dnsEndpoint", client.ObjectKeyFromObject(dnsEndpoint))
		if err := r.Delete(ctx, dnsEndpoint); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func serviceDNSEndpointName(serviceName string, dnsName string, recordType string) string {
	dnsNameHash := md5.Sum([]byte(strings.ToLower(strings.TrimSuffix(dnsName, "."))))
	suffix := fmt.Sprintf("-%x-%s", dnsNameHash[:6], strings.ToLower(recordType))
	maxServiceNameLength := validation.DNS1123SubdomainMaxLength - len(suffix)
	if len(serviceName) > maxServiceNameLength {
		serviceName = strings.TrimRight(serviceName[:maxServiceNameLength], "-.")
	}

	return serviceName + suffix
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Service{}).
		Owns(&infrastructurev1alpha1.DNSEndpoint{}).
		Watches(&infrastructurev1alpha1.Zone{}, handler.EnqueueRequestsFromMapFunc(r.servicesForZone)).
		Complete(r)
}

func (r *ServiceReconciler) servicesForZone(ctx context.Context, object client.Object) []reconcile.Request {
	services := &infrastructurev1alpha1.ServiceList{}
	if err := r.List(ctx, services); err != nil {
		logf.FromContext(ctx).Error(err, "Unable to list services for zone change")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(services.Items))
	for i := range services.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&services.Items[i])})
	}
	return requests
}
