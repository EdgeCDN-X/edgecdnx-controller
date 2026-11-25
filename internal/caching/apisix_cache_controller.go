/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package caching

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/controller"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
	TrustedProxies     []string
}

// APISIX's WASM host does not reliably update the special Cache-Control header.
const secureURLCacheGuard = `return function()
  ngx.header["Cache-Control"] = "private, no-store"
end`

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

	tlsErr := r.reconcileTLS(ctx, service)
	if tlsErr != nil {
		log.Error(tlsErr, "unable to reconcile APISIX TLS", "Service", service.Name)
		return ctrl.Result{}, tlsErr
	}

	if service.Status != (infrastructurev1alpha1.ServiceStatus{}) {
		if err := r.reconcileUpstreams(ctx, service); err != nil {
			log.Error(err, "unable to reconcile APISIX upstreams", "Service", service.Name)
			return ctrl.Result{}, err
		}
		if err := r.reconcileRoute(ctx, service); err != nil {
			log.Error(err, "unable to reconcile APISIX route", "Service", service.Name)
			return ctrl.Result{}, err
		}

		service.Status = infrastructurev1alpha1.ServiceStatus{
			Status: controller.HealthStatusHealthy,
		}
		return ctrl.Result{}, r.Status().Update(ctx, service)
	} else {
		service.Status = infrastructurev1alpha1.ServiceStatus{
			Status: controller.HealthStatusProgressing,
		}
		return ctrl.Result{}, r.Status().Update(ctx, service)
	}
}

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

	plugins := make([]apisixv2.ApisixRoutePlugin, 0)

	if len(service.Spec.SecureKeys) > 0 {
		keys := make([]map[string]string, 0, len(service.Spec.SecureKeys))
		for _, key := range service.Spec.SecureKeys {
			keys = append(keys, map[string]string{"name": key.Name, "value": key.Value})
		}
		conf := map[string]any{
			"secure_keys":      keys,
			"disable_prefixes": service.Spec.SecureURL.DisablePrefixes,
			"bind_client_ip":   service.Spec.SecureURL.BindClientIP,
			"trusted_proxies":  append([]string{}, r.TrustedProxies...),
		}
		data, err := json.Marshal(map[string]any{"conf": conf})
		if err != nil {
			return fmt.Errorf("marshal secure URL config: %w", err)
		}
		plugins = append(plugins, apisixv2.ApisixRoutePlugin{
			Name: "secure-urls", Enable: true,
			Config: apiextensionsv1.JSON{Raw: data},
		})
		guard, err := json.Marshal(map[string]any{
			"phase":     "header_filter",
			"functions": []string{secureURLCacheGuard},
		})
		if err != nil {
			return fmt.Errorf("marshal secure URL cache guard: %w", err)
		}
		plugins = append(plugins, apisixv2.ApisixRoutePlugin{
			Name: "serverless-post-function", Enable: true,
			Config: apiextensionsv1.JSON{Raw: guard},
		})
	}

	// request_uri is immutable; use the filtered args in the cache key.
	rawCacheConfig := map[string]any{
		"cache_zone":        service.Spec.Cache,
		"cache_key":         []string{"$host", "$uri", "$is_args", "$args"},
		"cache_method":      []string{"GET"},
		"cache_http_status": []int{200},
		"cache_strategy":    "disk",
		"cache_control":     true,
	}

	jsonData, err := json.Marshal(rawCacheConfig)
	if err != nil {
		return fmt.Errorf("marshal cache config: %w", err)
	}

	caching := apisixv2.ApisixRoutePlugin{
		Name:   "proxy-cache",
		Enable: true,
		Config: apiextensionsv1.JSON{
			Raw: jsonData,
		},
	}
	plugins = append(plugins, caching)

	if cors := service.Spec.Cors; cors != (infrastructurev1alpha1.CorsSpec{}) {
		origins := normalizeCORSList(cors.AllowOrigin)
		methods := normalizeCORSList(cors.AllowMethods)
		if cors.AllowCredentials && (origins == "" || origins == "*" || methods == "" || methods == "*") {
			return fmt.Errorf("APISIX CORS with credentials requires explicit allowOrigin and allowMethods (not *)")
		}

		corsConfig := map[string]any{"allow_credential": cors.AllowCredentials}
		if origins != "" {
			corsConfig["allow_origins"] = origins
		}
		if methods != "" {
			corsConfig["allow_methods"] = methods
		}
		if cors.AllowCredentials {
			corsConfig["allow_headers"] = "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization"
		}
		data, err := json.Marshal(corsConfig)
		if err != nil {
			return fmt.Errorf("marshal CORS config: %w", err)
		}
		plugins = append(plugins, apisixv2.ApisixRoutePlugin{
			Name:   "cors",
			Enable: true,
			Config: apiextensionsv1.JSON{Raw: data},
		})
	}

	if len(service.Spec.Path.Rewrites) > 0 {
		regexURI := make([]string, 0, 2*len(service.Spec.Path.Rewrites))
		for _, rule := range service.Spec.Path.Rewrites {
			regexURI = append(regexURI, rule.Pattern, rule.Replacement)
		}
		data, err := json.Marshal(map[string]any{"regex_uri": regexURI})
		if err != nil {
			return fmt.Errorf("marshal proxy rewrite config: %w", err)
		}
		plugins = append(plugins, apisixv2.ApisixRoutePlugin{
			Name:   "proxy-rewrite",
			Enable: true,
			Config: apiextensionsv1.JSON{Raw: data},
		})
	}

	queryParams := service.Spec.CacheKeySpec.QueryParams
	if queryParams == nil {
		queryParams = []string{}
	}
	allowlist, err := json.Marshal(queryParams)
	if err != nil {
		return fmt.Errorf("marshal query parameter allowlist: %w", err)
	}
	filterConfig, err := json.Marshal(map[string]any{
		"conf": string(allowlist),
	})
	if err != nil {
		return fmt.Errorf("marshal query parameter filter config: %w", err)
	}
	plugins = append(plugins, apisixv2.ApisixRoutePlugin{
		Name:   "query-param-filter",
		Enable: true,
		Config: apiextensionsv1.JSON{
			Raw: filterConfig,
		},
	})

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
				Plugins:   plugins,
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

func normalizeCORSList(value string) string {
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ",")
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
		Owns(&apisixv2.ApisixTls{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
