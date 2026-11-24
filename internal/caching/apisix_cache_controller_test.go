package caching

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestApacheApisixRouteRewrites(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := apisixv2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	service := &infrastructurev1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "edge-service", Namespace: "tenant"},
		Spec: infrastructurev1alpha1.ServiceSpec{
			OriginType:    infrastructurev1alpha1.OriginTypeStatic,
			StaticOrigins: []infrastructurev1alpha1.StaticOriginSpec{{Upstream: "origin.example"}},
			Cache:         "apisix",
			Cors:          infrastructurev1alpha1.CorsSpec{AllowOrigin: "https://example.com"},
			Path: infrastructurev1alpha1.PathSpec{
				Paths:   []string{"/images/*", "/files/*"},
				Rewrite: "/legacy",
			},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).Build()
	reconciler := &ApacheApisixCacheReconciler{Client: k8sClient, Scheme: scheme}

	check := func(want []string) {
		t.Helper()
		if err := reconciler.reconcileRoute(ctx, service); err != nil {
			t.Fatal(err)
		}
		route := &apisixv2.ApisixRoute{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(service), route); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(route.Spec.HTTP[0].Match.Paths, service.Spec.Path.Paths) {
			t.Fatalf("route paths changed: %v", route.Spec.HTTP[0].Match.Paths)
		}
		plugins := make(map[string]apisixv2.ApisixRoutePlugin)
		for _, plugin := range route.Spec.HTTP[0].Plugins {
			plugins[plugin.Name] = plugin
		}
		if !plugins["proxy-cache"].Enable || !plugins["cors"].Enable {
			t.Fatal("proxy-cache and CORS plugins should be preserved")
		}
		plugin, found := plugins["proxy-rewrite"]
		if want == nil {
			if found {
				t.Fatal("proxy-rewrite should be absent")
			}
			return
		}
		if !found || !plugin.Enable {
			t.Fatal("proxy-rewrite should be enabled")
		}
		var config struct {
			RegexURI []string `json:"regex_uri"`
		}
		if err := json.Unmarshal(plugin.Config.Raw, &config); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(config.RegexURI, want) {
			t.Fatalf("regex_uri = %#v, want %#v", config.RegexURI, want)
		}
	}

	check(nil)
	service.Spec.Path.Rewrites = []infrastructurev1alpha1.PathRewriteRule{
		{Pattern: "^/images/(.*)$", Replacement: "/assets/$1"},
		{Pattern: "^/files/(.*)$", Replacement: "/$1"},
	}
	check([]string{"^/images/(.*)$", "/assets/$1", "^/files/(.*)$", "/$1"})
	service.Spec.Path.Rewrites = []infrastructurev1alpha1.PathRewriteRule{
		{Pattern: "^/files/(.*)$", Replacement: "/new/$1"},
		{Pattern: "^/images/(.*)$", Replacement: "/assets/$1"},
	}
	check([]string{"^/files/(.*)$", "/new/$1", "^/images/(.*)$", "/assets/$1"})
	service.Spec.Path.Rewrites = nil
	check(nil)
}

func TestApacheApisixRouteCORS(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := apisixv2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	service := &infrastructurev1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "edge-service", Namespace: "tenant"},
		Spec: infrastructurev1alpha1.ServiceSpec{
			OriginType:    infrastructurev1alpha1.OriginTypeStatic,
			StaticOrigins: []infrastructurev1alpha1.StaticOriginSpec{{Upstream: "origin.example"}},
			Cache:         "apisix",
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).Build()
	reconciler := &ApacheApisixCacheReconciler{Client: k8sClient, Scheme: scheme}

	check := func(want map[string]any) {
		t.Helper()
		if err := reconciler.reconcileRoute(ctx, service); err != nil {
			t.Fatal(err)
		}
		route := &apisixv2.ApisixRoute{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(service), route); err != nil {
			t.Fatal(err)
		}
		var found *apisixv2.ApisixRoutePlugin
		cacheFound := false
		for i := range route.Spec.HTTP[0].Plugins {
			plugin := &route.Spec.HTTP[0].Plugins[i]
			if plugin.Name == "cors" {
				found = plugin
			}
			if plugin.Name == "proxy-cache" && plugin.Enable {
				cacheFound = true
			}
		}
		if !cacheFound {
			t.Fatal("proxy-cache plugin should be preserved")
		}
		if want == nil {
			if found != nil {
				t.Fatal("CORS plugin should be absent")
			}
			return
		}
		if found == nil || !found.Enable {
			t.Fatal("CORS plugin should be enabled")
		}
		var config map[string]any
		if err := json.Unmarshal(found.Config.Raw, &config); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(config, want) {
			t.Fatalf("CORS config = %#v, want %#v", config, want)
		}
	}

	check(nil)
	service.Spec.Cors = infrastructurev1alpha1.CorsSpec{
		AllowOrigin:      "https://one.example, https://two.example",
		AllowMethods:     "GET, POST",
		AllowCredentials: true,
	}
	check(map[string]any{
		"allow_origins":    "https://one.example,https://two.example",
		"allow_methods":    "GET,POST",
		"allow_credential": true,
		"allow_headers":    "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
	})
	service.Spec.Cors = infrastructurev1alpha1.CorsSpec{AllowOrigin: "https://one.example"}
	check(map[string]any{"allow_origins": "https://one.example", "allow_credential": false})

	for _, cors := range []infrastructurev1alpha1.CorsSpec{
		{AllowCredentials: true},
		{AllowCredentials: true, AllowOrigin: "*", AllowMethods: "GET"},
		{AllowCredentials: true, AllowOrigin: "https://one.example", AllowMethods: "*"},
		{AllowCredentials: true, AllowOrigin: "https://one.example"},
	} {
		service.Spec.Cors = cors
		if err := reconciler.reconcileRoute(ctx, service); err == nil || !strings.Contains(err.Error(), "requires explicit") {
			t.Fatalf("expected credentialed wildcard/default error for %+v, got %v", cors, err)
		}
	}
	service.Spec.Cors = infrastructurev1alpha1.CorsSpec{AllowOrigin: "https://one.example"}
	check(map[string]any{"allow_origins": "https://one.example", "allow_credential": false})
	service.Spec.Cors = infrastructurev1alpha1.CorsSpec{}
	check(nil)
}

func TestApacheApisixCacheReconcilerReconcilesUpstreamList(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := apisixv2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	service := &infrastructurev1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "edge-service", Namespace: "tenant"},
		Spec: infrastructurev1alpha1.ServiceSpec{
			OriginType: infrastructurev1alpha1.OriginTypeStatic,
			Domain:     "www.example.com",
			Cache:      "apache-apisix",
			Path:       infrastructurev1alpha1.PathSpec{Paths: []string{"/", "/images/*"}},
			HostAliases: []infrastructurev1alpha1.HostAliasSpec{
				{Name: "cdn.example.com"},
			},
			StaticOrigins: []infrastructurev1alpha1.StaticOriginSpec{
				{Upstream: "origin-a.example", Port: 8080, Scheme: "Http"},
				{Upstream: "origin-b.example", Port: 8443, Scheme: "Https", HostHeader: "backend.example"},
			},
		},
		Status: infrastructurev1alpha1.ServiceStatus{Status: "Progressing"},
	}
	stale := &apisixv2.ApisixUpstream{ObjectMeta: metav1.ObjectMeta{
		Name: "edge-service-upstream-2", Namespace: service.Namespace,
	}}
	if err := controllerutil.SetControllerReference(service, stale, scheme); err != nil {
		t.Fatal(err)
	}
	unmanaged := &apisixv2.ApisixUpstream{ObjectMeta: metav1.ObjectMeta{
		Name: "unmanaged-upstream", Namespace: service.Namespace,
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service, stale, unmanaged).Build()
	reconciler := &ApacheApisixCacheReconciler{Client: k8sClient, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(service)}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}

	firstKey := client.ObjectKey{Namespace: service.Namespace, Name: "edge-service-upstream-0"}
	first := &apisixv2.ApisixUpstream{}
	if err := k8sClient.Get(ctx, firstKey, first); err != nil {
		t.Fatal(err)
	}
	if !metav1.IsControlledBy(first, service) {
		t.Fatal("expected generated upstream to be controlled by its Service")
	}
	if first.Spec.Scheme != "http" || len(first.Spec.ExternalNodes) != 1 ||
		first.Spec.ExternalNodes[0].Name != "origin-a.example" {
		t.Fatalf("unexpected first upstream spec: %+v", first.Spec)
	}

	secondKey := client.ObjectKey{Namespace: service.Namespace, Name: "edge-service-upstream-1"}
	second := &apisixv2.ApisixUpstream{}
	if err := k8sClient.Get(ctx, secondKey, second); err != nil {
		t.Fatal(err)
	}
	if second.Spec.Scheme != "https" || second.Spec.PassHost != "rewrite" ||
		second.Spec.UpstreamHost != "backend.example" {
		t.Fatalf("unexpected second upstream spec: %+v", second.Spec)
	}
	route := &apisixv2.ApisixRoute{}
	routeKey := client.ObjectKey{Namespace: service.Namespace, Name: service.Name}
	if err := k8sClient.Get(ctx, routeKey, route); err != nil {
		t.Fatal(err)
	}
	if !metav1.IsControlledBy(route, service) {
		t.Fatal("expected generated route to be controlled by its Service")
	}
	if route.Spec.IngressClassName != "apisix" || len(route.Spec.HTTP) != 1 {
		t.Fatalf("unexpected route spec: %+v", route.Spec)
	}
	httpRoute := route.Spec.HTTP[0]
	if len(httpRoute.Match.Paths) != 2 || httpRoute.Match.Paths[1] != "/images/*" ||
		len(httpRoute.Match.Hosts) != 2 || httpRoute.Match.Hosts[0] != "www.example.com" ||
		httpRoute.Match.Hosts[1] != "cdn.example.com" {
		t.Fatalf("unexpected route match: %+v", httpRoute.Match)
	}
	if len(httpRoute.Upstreams) != 2 ||
		httpRoute.Upstreams[0].Name != "edge-service-upstream-0" ||
		httpRoute.Upstreams[1].Name != "edge-service-upstream-1" ||
		len(httpRoute.Plugins) != 0 {
		t.Fatalf("unexpected route upstreams or plugins: %+v", httpRoute)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(stale), &apisixv2.ApisixUpstream{}); !apierrors.IsNotFound(err) {
		t.Fatal("stale controlled upstream was not deleted")
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(unmanaged), &apisixv2.ApisixUpstream{}); err != nil {
		t.Fatalf("unmanaged upstream was deleted: %v", err)
	}

	currentService := &infrastructurev1alpha1.Service{}
	if err := k8sClient.Get(ctx, request.NamespacedName, currentService); err != nil {
		t.Fatal(err)
	}
	currentService.Spec.StaticOrigins = currentService.Spec.StaticOrigins[:1]
	currentService.Spec.StaticOrigins[0].Upstream = "updated-origin.example"
	currentService.Spec.Cache = "apisix-edge"
	currentService.Spec.Path.Paths = []string{"/updated/*"}
	if err := k8sClient.Update(ctx, currentService); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}

	if err := k8sClient.Get(ctx, firstKey, first); err != nil {
		t.Fatal(err)
	}
	if first.Spec.ExternalNodes[0].Name != "updated-origin.example" {
		t.Fatalf("upstream was not updated from Service spec: %+v", first.Spec.ExternalNodes)
	}
	if err := k8sClient.Get(ctx, secondKey, second); !apierrors.IsNotFound(err) {
		t.Fatal("upstream removed from Service list was not deleted")
	}
	if err := k8sClient.Get(ctx, routeKey, route); err != nil {
		t.Fatal(err)
	}
	if route.Spec.IngressClassName != "apisix" ||
		len(route.Spec.HTTP[0].Match.Paths) != 1 ||
		route.Spec.HTTP[0].Match.Paths[0] != "/updated/*" ||
		len(route.Spec.HTTP[0].Upstreams) != 1 ||
		route.Spec.HTTP[0].Upstreams[0].Name != "edge-service-upstream-0" {
		t.Fatalf("route was not updated from Service spec: %+v", route.Spec)
	}

	currentService.Spec.StaticOrigins = nil
	if err := k8sClient.Update(ctx, currentService); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := k8sClient.Get(ctx, routeKey, route); !apierrors.IsNotFound(err) {
		t.Fatalf("route was not deleted when the origin list became empty: %v", err)
	}
}
