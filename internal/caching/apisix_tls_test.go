package caching

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	apisixv2 "github.com/apache/apisix-ingress-controller/api/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testTLSCertificate(t *testing.T, secretName string, hosts ...string) api.CertificateSpec {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     hosts,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return api.CertificateSpec{
		SecretRef: secretName,
		Crt:       base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		Key:       base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})),
	}
}

func apisixTLSTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, register := range []func(*runtime.Scheme) error{api.AddToScheme, apisixv2.AddToScheme, corev1.AddToScheme} {
		if err := register(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return scheme
}

func TestAPISIXTLSReconcileLifecycle(t *testing.T) {
	ctx := context.Background()
	scheme := apisixTLSTestScheme(t)
	shared := testTLSCertificate(t, "shared-tls", "www.example.com", "shared.example.com")
	alias := testTLSCertificate(t, "alias-tls", "alias.example.com")
	service := &api.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "edge", Namespace: "tenant", UID: "service-uid"},
		Spec: api.ServiceSpec{
			Domain: "www.example.com", Cache: "apisix-edge", Certificate: shared,
			HostAliases: []api.HostAliasSpec{
				{Name: "shared.example.com", Certificate: shared},
				{Name: "alias.example.com", Certificate: alias},
				{Name: "pending.example.com", Certificate: api.CertificateSpec{SecretRef: "pending"}},
			},
		},
	}
	unmanaged := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unmanaged", Namespace: service.Namespace}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service, unmanaged).
		WithStatusSubresource(&api.Service{}).Build()
	r := &ApacheApisixCacheReconciler{Client: c, Scheme: scheme}
	serviceKey := client.ObjectKeyFromObject(service)
	request := ctrl.Request{NamespacedName: serviceKey}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}

	for _, item := range []struct {
		host, secret string
		cert         api.CertificateSpec
	}{
		{"www.example.com", "shared-tls", shared},
		{"shared.example.com", "shared-tls", shared},
		{"alias.example.com", "alias-tls", alias},
	} {
		secret := &corev1.Secret{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: item.secret}, secret); err != nil {
			t.Fatal(err)
		}
		if secret.Type != corev1.SecretTypeTLS || !metav1.IsControlledBy(secret, service) ||
			base64.StdEncoding.EncodeToString(secret.Data[corev1.TLSCertKey]) != item.cert.Crt ||
			base64.StdEncoding.EncodeToString(secret.Data[corev1.TLSPrivateKeyKey]) != item.cert.Key {
			t.Fatalf("incorrect TLS secret for %s", item.host)
		}
		resource := &apisixv2.ApisixTls{}
		key := client.ObjectKey{Namespace: service.Namespace, Name: apisixTLSName(service.Name, item.host)}
		if err := c.Get(ctx, key, resource); err != nil {
			t.Fatal(err)
		}
		if !metav1.IsControlledBy(resource, service) || resource.Spec.IngressClassName != "apisix-edge" ||
			len(resource.Spec.Hosts) != 1 || string(resource.Spec.Hosts[0]) != item.host ||
			resource.Spec.Secret != (apisixv2.ApisixSecret{Name: item.secret, Namespace: service.Namespace}) {
			t.Fatalf("incorrect ApisixTls spec: %+v", resource.Spec)
		}
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: "pending"}, &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Fatalf("pending certificate unexpectedly created a secret: %v", err)
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: "shared-tls"}, secret); err != nil {
		t.Fatal(err)
	}
	tlsObject := &apisixv2.ApisixTls{}
	tlsKey := client.ObjectKey{Namespace: service.Namespace, Name: apisixTLSName(service.Name, "www.example.com")}
	if err := c.Get(ctx, tlsKey, tlsObject); err != nil {
		t.Fatal(err)
	}
	secretVersion, tlsVersion := secret.ResourceVersion, tlsObject.ResourceVersion
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil || secret.ResourceVersion != secretVersion {
		t.Fatalf("unchanged secret was updated: %v", err)
	}
	if err := c.Get(ctx, tlsKey, tlsObject); err != nil || tlsObject.ResourceVersion != tlsVersion {
		t.Fatalf("unchanged ApisixTls was updated: %v", err)
	}

	secret.Data[corev1.TLSCertKey] = []byte("drift")
	if err := c.Update(ctx, secret); err != nil {
		t.Fatal(err)
	}
	tlsObject.Spec.Hosts = []apisixv2.HostType{"drift.example.com"}
	if err := c.Update(ctx, tlsObject); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, tlsKey, tlsObject); err != nil || string(tlsObject.Spec.Hosts[0]) != "www.example.com" {
		t.Fatalf("ApisixTls drift was not corrected: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil ||
		base64.StdEncoding.EncodeToString(secret.Data[corev1.TLSCertKey]) != shared.Crt {
		t.Fatalf("secret drift was not corrected: %v", err)
	}

	if err := c.Get(ctx, serviceKey, service); err != nil {
		t.Fatal(err)
	}
	renewedShared := testTLSCertificate(t, "shared-tls", "www.example.com", "shared.example.com")
	service.Spec.Certificate = renewedShared
	service.Spec.HostAliases[0].Certificate = renewedShared
	if err := c.Update(ctx, service); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil ||
		base64.StdEncoding.EncodeToString(secret.Data[corev1.TLSCertKey]) != renewedShared.Crt {
		t.Fatalf("renewed certificate did not update its Secret: %v", err)
	}
	if err := c.Get(ctx, serviceKey, service); err != nil {
		t.Fatal(err)
	}
	renewed := testTLSCertificate(t, "new-tls", "www.example.com")
	service.Spec.Certificate = renewed
	service.Spec.HostAliases = nil
	if err := c.Update(ctx, service); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, tlsKey, tlsObject); err != nil || tlsObject.Spec.Secret.Name != "new-tls" {
		t.Fatalf("certificate renewal did not update ApisixTls: %v", err)
	}
	for _, name := range []string{"shared-tls", "alias-tls"} {
		if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: name}, &corev1.Secret{}); !apierrors.IsNotFound(err) {
			t.Fatalf("stale secret %q remains: %v", name, err)
		}
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: apisixTLSName(service.Name, "alias.example.com")}, &apisixv2.ApisixTls{}); !apierrors.IsNotFound(err) {
		t.Fatalf("stale ApisixTls remains: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(unmanaged), &corev1.Secret{}); err != nil {
		t.Fatalf("unmanaged secret was removed: %v", err)
	}
}

func TestAPISIXInvalidTLSDoesNotBlockRouting(t *testing.T) {
	ctx := context.Background()
	scheme := apisixTLSTestScheme(t)
	certificate := testTLSCertificate(t, "", "www.example.com")
	service := &api.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "edge", Namespace: "tenant", UID: "uid"},
		Spec: api.ServiceSpec{
			Domain: "www.example.com", Certificate: certificate, OriginType: api.OriginTypeStatic,
			StaticOrigins: []api.StaticOriginSpec{{Upstream: "origin.example.com", Port: 80, Scheme: "Http"}},
		},
		Status: api.ServiceStatus{Status: "Progressing"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).Build()
	r := &ApacheApisixCacheReconciler{Client: c, Scheme: scheme}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(service)}); err == nil || !strings.Contains(err.Error(), "valid secretRef") {
		t.Fatalf("expected a missing secretRef error, got %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(service), &apisixv2.ApisixRoute{}); err != nil {
		t.Fatalf("invalid TLS blocked route reconciliation: %v", err)
	}
}

func TestAPISIXTLSRejectsInvalidInputsAndCollisions(t *testing.T) {
	valid := testTLSCertificate(t, "service-tls", "www.example.com")
	other := testTLSCertificate(t, "service-tls", "www.example.com", "alias.example.com")
	tests := []struct {
		name        string
		cert        api.CertificateSpec
		alias       *api.HostAliasSpec
		unmanaged   client.Object
		wantMessage string
	}{
		{name: "missing secretRef", cert: api.CertificateSpec{Crt: valid.Crt, Key: valid.Key}, wantMessage: "valid secretRef"},
		{name: "missing key", cert: api.CertificateSpec{SecretRef: valid.SecretRef, Crt: valid.Crt}, wantMessage: "both crt and key"},
		{name: "invalid base64", cert: api.CertificateSpec{SecretRef: valid.SecretRef, Crt: "!", Key: valid.Key}, wantMessage: "decode certificate"},
		{name: "mismatched key", cert: api.CertificateSpec{SecretRef: valid.SecretRef, Crt: valid.Crt, Key: other.Key}, wantMessage: "invalid TLS certificate or key"},
		{name: "wrong hostname", cert: testTLSCertificate(t, "service-tls", "other.example.com"), wantMessage: "does not cover"},
		{name: "conflicting shared secret", cert: valid, alias: &api.HostAliasSpec{Name: "alias.example.com", Certificate: other}, wantMessage: "used by different TLS certificates"},
		{name: "unmanaged secret", cert: valid, unmanaged: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "service-tls", Namespace: "tenant"}}, wantMessage: "not managed"},
		{name: "unmanaged ApisixTls", cert: valid, unmanaged: &apisixv2.ApisixTls{ObjectMeta: metav1.ObjectMeta{Name: apisixTLSName("edge", "www.example.com"), Namespace: "tenant"}}, wantMessage: "not managed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := apisixTLSTestScheme(t)
			service := &api.Service{ObjectMeta: metav1.ObjectMeta{Name: "edge", Namespace: "tenant", UID: "uid"},
				Spec: api.ServiceSpec{Domain: "www.example.com", Certificate: tt.cert}}
			if tt.alias != nil {
				service.Spec.HostAliases = []api.HostAliasSpec{*tt.alias}
			}
			objects := []client.Object{service}
			if tt.unmanaged != nil {
				objects = append(objects, tt.unmanaged)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			r := &ApacheApisixCacheReconciler{Client: c, Scheme: scheme}
			if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(service)}); err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("expected error containing %q, got %v", tt.wantMessage, err)
			}
			if tt.unmanaged != nil {
				if err := c.Get(ctx, client.ObjectKeyFromObject(tt.unmanaged), tt.unmanaged); err != nil {
					t.Fatalf("unmanaged resource was changed: %v", err)
				}
			}

			secrets := &corev1.SecretList{}
			if err := c.List(ctx, secrets, client.MatchingLabels{apisixTLSManagedLabel: "true"}); err != nil || len(secrets.Items) != 0 {
				t.Fatalf("invalid input created managed secrets: %v", err)
			}
		})
	}
}
