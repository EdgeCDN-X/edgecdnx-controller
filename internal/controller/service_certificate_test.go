package controller

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestServiceCertificates(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, register := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme, certmanagerv1.AddToScheme} {
		if err := register(scheme); err != nil {
			t.Fatal(err)
		}
	}
	service := &api.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "cdn", Namespace: "customer", UID: "service-uid"},
		Spec: api.ServiceSpec{Domain: "cdn.example.test", HostAliases: []api.HostAliasSpec{
			{Name: "cname.example.test", TargetType: "CNAME"},
			{Name: "delegated.example.test", TargetType: "DELEGATION"},
			{Name: "legacy.example.test"},
			{Name: "custom.example.test", TargetType: "CNAME", Certificate: api.CertificateSpec{Crt: "custom-crt", Key: "custom-key"}},
		}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).
		WithIndex(&certmanagerv1.Certificate{}, certificateSecretIndex, func(object client.Object) []string {
			return []string{object.(*certmanagerv1.Certificate).Spec.SecretName}
		}).WithStatusSubresource(&certmanagerv1.Certificate{}).Build()
	r := &ServiceReconciler{Client: c, Scheme: scheme, ClusterIssuerName: "custom-issuer"}
	serviceKey := client.ObjectKeyFromObject(service)
	run := func() error {
		if err := c.Get(ctx, serviceKey, service); err != nil {
			return err
		}
		return r.reconcileServiceCertificates(ctx, service)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	names := []string{"cdn.example.test", "cname.example.test", "delegated.example.test", "legacy.example.test"}
	for i, hostname := range names {
		name := serviceDNSEndpointName(service.Name, hostname, "cert")
		certificate := &certmanagerv1.Certificate{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: name}, certificate); err != nil {
			t.Fatal(err)
		}
		target := "delegation"
		if i == 1 {
			target = "cname"
		}
		if !metav1.IsControlledBy(certificate, service) || certificate.Labels[solverTargetLabel] != target ||
			certificate.Spec.IssuerRef != (cmmeta.ObjectReference{Name: "custom-issuer", Kind: "ClusterIssuer", Group: "cert-manager.io"}) ||
			certificate.Spec.SecretName != name || len(certificate.Spec.DNSNames) != 1 || certificate.Spec.DNSNames[0] != hostname {
			t.Fatalf("incorrect certificate: %+v", certificate)
		}
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: serviceDNSEndpointName(service.Name, "custom.example.test", "cert")}, &certmanagerv1.Certificate{}); !apierrors.IsNotFound(err) {
		t.Fatalf("user-provided certificate should not be replaced: %v", err)
	}

	primaryName := serviceDNSEndpointName(service.Name, names[0], "cert")
	aliasName := serviceDNSEndpointName(service.Name, names[1], "cert")
	for _, name := range []string{primaryName, aliasName} {
		certificate := &certmanagerv1.Certificate{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: name}, certificate); err != nil {
			t.Fatal(err)
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: service.Namespace},
			Data: map[string][]byte{corev1.TLSCertKey: []byte("certificate-" + name), corev1.TLSPrivateKeyKey: []byte("private-" + name)}}
		if err := c.Create(ctx, secret); err != nil {
			t.Fatal(err)
		}
		certificate.Status.Conditions = []certmanagerv1.CertificateCondition{{
			Type: certmanagerv1.CertificateConditionReady, Status: cmmeta.ConditionTrue, ObservedGeneration: certificate.Generation,
		}}
		if err := c.Status().Update(ctx, certificate); err != nil {
			t.Fatal(err)
		}
		requests := r.servicesForCertificateSecret(ctx, secret)
		if len(requests) != 1 || requests[0].NamespacedName != serviceKey {
			t.Fatalf("secret watch mapped to %v", requests)
		}
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, serviceKey, service); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		got  api.CertificateSpec
		name string
	}{{service.Spec.Certificate, primaryName}, {service.Spec.HostAliases[0].Certificate, aliasName}} {
		if pair.got.CertificateRef != pair.name || pair.got.SecretRef != pair.name ||
			pair.got.Crt != base64.StdEncoding.EncodeToString([]byte("certificate-"+pair.name)) ||
			pair.got.Key != base64.StdEncoding.EncodeToString([]byte("private-"+pair.name)) {
			t.Fatalf("unexpected copied certificate for %s", pair.name)
		}
	}
	if service.Spec.HostAliases[3].Certificate.Crt != "custom-crt" {
		t.Fatal("user certificate was modified")
	}
	version := service.ResourceVersion
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, serviceKey, service); err != nil || service.ResourceVersion != version {
		t.Fatalf("unchanged certificates patched the service: %v", err)
	}

	local := fake.NewClientBuilder().WithScheme(scheme).Build()
	downstream := newTestDownstreamReconciler(t, c, local, DownstreamTargetCache)
	queueKey := downstreamQueueKey{Kind: "Service", Key: serviceKey}
	if err := downstream.reconcileKey(ctx, queueKey); err != nil {
		t.Fatal(err)
	}
	beforeRenewal := service.DeepCopy()
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: aliasName}, secret); err != nil {
		t.Fatal(err)
	}
	secret.Data[corev1.TLSCertKey] = []byte("renewed")
	if err := c.Update(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, serviceKey, service); err != nil {
		t.Fatal(err)
	}
	if service.Spec.HostAliases[0].Certificate.Crt != base64.StdEncoding.EncodeToString([]byte("renewed")) {
		t.Fatal("renewed certificate not copied")
	}
	if downstreamSourceEqual(beforeRenewal, service) {
		t.Fatal("renewal should enqueue a downstream Service update")
	}
	if err := downstream.reconcileKey(ctx, queueKey); err != nil {
		t.Fatal(err)
	}
	mirror := &api.Service{}
	if err := local.Get(ctx, serviceKey, mirror); err != nil {
		t.Fatal(err)
	}
	if mirror.Spec.Certificate != service.Spec.Certificate || mirror.Spec.HostAliases[0].Certificate != service.Spec.HostAliases[0].Certificate {
		t.Fatal("certificate material was not mirrored downstream")
	}
	service.Spec.HostAliases[0].TargetType = "DELEGATION"
	service.Spec.HostAliases = service.Spec.HostAliases[:2]
	if err := c.Update(ctx, service); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	alias := &certmanagerv1.Certificate{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: aliasName}, alias); err != nil || alias.Labels[solverTargetLabel] != "delegation" {
		t.Fatalf("solver label not updated: %v", err)
	}
	stale := serviceDNSEndpointName(service.Name, names[3], "cert")
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: stale}, &certmanagerv1.Certificate{}); !apierrors.IsNotFound(err) {
		t.Fatalf("stale certificate not removed: %v", err)
	}
}

func TestServiceCertificateReadinessAndOwnership(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, register := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme, certmanagerv1.AddToScheme} {
		if err := register(scheme); err != nil {
			t.Fatal(err)
		}
	}
	service := &api.Service{ObjectMeta: metav1.ObjectMeta{Name: "cdn", Namespace: "customer", UID: "uid"}, Spec: api.ServiceSpec{Domain: "cdn.example.test"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).WithStatusSubresource(&certmanagerv1.Certificate{}).Build()
	r := &ServiceReconciler{Client: c, Scheme: scheme}
	if err := r.reconcileServiceCertificates(ctx, service); err != nil {
		t.Fatal(err)
	}
	name := serviceDNSEndpointName(service.Name, service.Spec.Domain, "cert")
	cert := &certmanagerv1.Certificate{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: name}, cert); err != nil {
		t.Fatal(err)
	}
	if cert.Spec.IssuerRef.Name != "edgecdnx" {
		t.Fatalf("unexpected default issuer: %s", cert.Spec.IssuerRef.Name)
	}
	cert.Generation = 2
	if err := c.Update(ctx, cert); err != nil {
		t.Fatal(err)
	}
	cert.Status.Conditions = []certmanagerv1.CertificateCondition{{Type: certmanagerv1.CertificateConditionReady, Status: cmmeta.ConditionTrue, ObservedGeneration: 1}}
	if err := c.Status().Update(ctx, cert); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileServiceCertificates(ctx, service); err != nil {
		t.Fatalf("stale readiness must not read the secret: %v", err)
	}
	cert.Status.Conditions[0].ObservedGeneration = 2
	if err := c.Status().Update(ctx, cert); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileServiceCertificates(ctx, service); err == nil || !strings.Contains(err.Error(), "get secret for ready certificate") {
		t.Fatalf("missing secret should be reported: %v", err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: service.Namespace},
		Data: map[string][]byte{corev1.TLSCertKey: []byte("certificate")}}
	if err := c.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileServiceCertificates(ctx, service); err == nil || !strings.Contains(err.Error(), "missing tls.crt or tls.key") {
		t.Fatalf("incomplete secret should be reported: %v", err)
	}
	if err := c.Delete(ctx, cert); err != nil {
		t.Fatal(err)
	}
	unowned := &certmanagerv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: service.Namespace}}
	if err := c.Create(ctx, unowned); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileServiceCertificates(ctx, service); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("unowned certificate should not be adopted: %v", err)
	}
}
