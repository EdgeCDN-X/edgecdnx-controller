/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package caching

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/controller"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ApacheApisixCacheReconciler reconciles a Service object
type ApacheApisixCacheReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	SecureUrlsEndpoint string
}

func (r *ApacheApisixCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	service := &infrastructurev1alpha1.Service{}

	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Service")
			return ctrl.Result{}, err
		}
		log.Info("Service resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	if !service.DeletionTimestamp.IsZero() {
		log.Info("Service is being deleted, ignoring reconcile loop")
		return ctrl.Result{}, nil
	}

	if service.Status != (infrastructurev1alpha1.ServiceStatus{}) {
		log.Info("Service has a non-empty status")

		if err := r.reconcileUpstreams(ctx, service); err != nil {
			log.Error(err, "unable to reconcile APISIX upstreams", "Service", service.Name)
			return ctrl.Result{}, err
		}
		if err := r.reconcileRoute(ctx, service); err != nil {
			log.Error(err, "unable to reconcile APISIX route", "Service", service.Name)
			return ctrl.Result{}, err
		}

	} else {
		service.Status = infrastructurev1alpha1.ServiceStatus{
			Status: controller.HealthStatusProgressing,
		}
		return ctrl.Result{}, r.Status().Update(ctx, service)
	}

	return ctrl.Result{}, nil
}

// +kubebuilder:rbac:groups=apisix.apache.org,resources=apisixroutes;apisixupstreams,verbs=get;list;watch;create;update;patch;delete
func (r *ApacheApisixCacheReconciler) reconcileUpstreams(ctx context.Context, service *infrastructurev1alpha1.Service) error {
	desiredNames := make(map[string]struct{}, len(service.Spec.StaticOrigins))
	if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeStatic {
		for i, origin := range service.Spec.StaticOrigins {
			name := apisixUpstreamName(service.Name, i)
			desiredNames[name] = struct{}{}
			desired, _, err := builder.APISixUpstreamBuilderFactory(name, service.Namespace).
				WithClass(service.Spec.Cache).
				WithUpstreams(origin).
				Build()
			if err != nil {
				return fmt.Errorf("build APISIX upstream %s: %w", name, err)
			}

			current := &apisixv2.ApisixUpstream{}
			key := client.ObjectKey{Namespace: service.Namespace, Name: name}
			if err := r.Get(ctx, key, current); err != nil {
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("get APISIX upstream %s: %w", key, err)
				}
				if err := controllerutil.SetControllerReference(service, &desired, r.Scheme); err != nil {
					return fmt.Errorf("set controller reference on APISIX upstream %s: %w", key, err)
				}
				if err := r.Create(ctx, &desired); err != nil {
					return fmt.Errorf("create APISIX upstream %s: %w", key, err)
				}
				continue
			}

			needsUpdate := !reflect.DeepEqual(current.Spec, desired.Spec) || !metav1.IsControlledBy(current, service)
			if !needsUpdate {
				continue
			}
			current.Spec = desired.Spec
			if err := controllerutil.SetControllerReference(service, current, r.Scheme); err != nil {
				return fmt.Errorf("set controller reference on APISIX upstream %s: %w", key, err)
			}
			if err := r.Update(ctx, current); err != nil {
				return fmt.Errorf("update APISIX upstream %s: %w", key, err)
			}
		}
	}

	upstreams := &apisixv2.ApisixUpstreamList{}
	if err := r.List(ctx, upstreams, client.InNamespace(service.Namespace)); err != nil {
		return fmt.Errorf("list APISIX upstreams in namespace %s: %w", service.Namespace, err)
	}
	for i := range upstreams.Items {
		upstream := &upstreams.Items[i]
		if !metav1.IsControlledBy(upstream, service) {
			continue
		}
		if _, desired := desiredNames[upstream.Name]; desired {
			continue
		}
		if err := r.Delete(ctx, upstream); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete stale APISIX upstream %s: %w", client.ObjectKeyFromObject(upstream), err)
		}
	}
	return nil
}

func (r *ApacheApisixCacheReconciler) reconcileRoute(ctx context.Context, service *infrastructurev1alpha1.Service) error {
	routeKey := client.ObjectKey{Namespace: service.Namespace, Name: service.Name}
	current := &apisixv2.ApisixRoute{}
	if service.Spec.OriginType != infrastructurev1alpha1.OriginTypeStatic || len(service.Spec.StaticOrigins) == 0 {
		if err := r.Get(ctx, routeKey, current); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("get APISIX route %s: %w", routeKey, err)
		}
		if !metav1.IsControlledBy(current, service) {
			return nil
		}
		if err := r.Delete(ctx, current); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete APISIX route %s: %w", routeKey, err)
		}
		return nil
	}

	paths := service.Spec.Path.Paths
	if len(paths) == 0 {
		paths = []string{"/"}
	}
	hosts := make([]string, 0, len(service.Spec.HostAliases)+1)
	if service.Spec.Domain != "" {
		hosts = append(hosts, service.Spec.Domain)
	}
	for _, alias := range service.Spec.HostAliases {
		hosts = append(hosts, alias.Name)
	}
	upstreams := make([]apisixv2.ApisixRouteUpstreamReference, 0, len(service.Spec.StaticOrigins))
	for i := range service.Spec.StaticOrigins {
		weight := 1
		upstreams = append(upstreams, apisixv2.ApisixRouteUpstreamReference{
			Name:   apisixUpstreamName(service.Name, i),
			Weight: &weight,
		})
	}
	desired := apisixv2.ApisixRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ApisixRoute",
			APIVersion: apisixv2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
		Spec: apisixv2.ApisixRouteSpec{
			IngressClassName: service.Spec.Cache,
			HTTP: []apisixv2.ApisixRouteHTTP{{
				Name: "service",
				Match: apisixv2.ApisixRouteHTTPMatch{
					Hosts: hosts,
					Paths: paths,
				},
				Upstreams: upstreams,
			}},
		},
	}

	if err := r.Get(ctx, routeKey, current); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get APISIX route %s: %w", routeKey, err)
		}
		if err := controllerutil.SetControllerReference(service, &desired, r.Scheme); err != nil {
			return fmt.Errorf("set controller reference on APISIX route %s: %w", routeKey, err)
		}
		if err := r.Create(ctx, &desired); err != nil {
			return fmt.Errorf("create APISIX route %s: %w", routeKey, err)
		}
		return nil
	}

	if reflect.DeepEqual(current.Spec, desired.Spec) && metav1.IsControlledBy(current, service) {
		return nil
	}
	current.Spec = desired.Spec
	if err := controllerutil.SetControllerReference(service, current, r.Scheme); err != nil {
		return fmt.Errorf("set controller reference on APISIX route %s: %w", routeKey, err)
	}
	if err := r.Update(ctx, current); err != nil {
		return fmt.Errorf("update APISIX route %s: %w", routeKey, err)
	}
	return nil
}

func apisixUpstreamName(serviceName string, index int) string {
	suffix := fmt.Sprintf("-upstream-%d", index)
	if len(serviceName)+len(suffix) <= validation.DNS1123SubdomainMaxLength {
		return serviceName + suffix
	}

	hash := sha256.Sum256([]byte(serviceName))
	hashSuffix := fmt.Sprintf("-%x", hash[:4])
	prefixLength := validation.DNS1123SubdomainMaxLength - len(suffix) - len(hashSuffix)
	return strings.TrimRight(serviceName[:prefixLength], "-.") + hashSuffix + suffix
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApacheApisixCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Service{}).
		Owns(&apisixv2.ApisixUpstream{}).
		Owns(&apisixv2.ApisixRoute{}).
		Complete(r)
}
