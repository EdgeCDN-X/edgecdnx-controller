package caching

import (
	"context"
	"testing"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestApacheApisixCacheReconcilerReconcilesUpstreamList(t *testing.T) {
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
