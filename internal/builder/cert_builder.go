package builder

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ICertificateBuilder interface {
	SetSecretName(secretName string)
	SetDNSNames(dnsNames []string)
	SetIssuerRef(issuerRef cmmeta.ObjectReference)
	SetRenewBefore(duration metav1.Duration)
	Build() (certmanagerv1.Certificate, string, error)
}

type DefaultCertBuilder struct {
	cert certmanagerv1.Certificate
}

func newDefaultCertBuilder(name string, namespace string, serviceName string) *DefaultCertBuilder {
	{
		return &DefaultCertBuilder{
			cert: certmanagerv1.Certificate{
				TypeMeta: metav1.TypeMeta{
					APIVersion: certmanagerv1.SchemeGroupVersion.String(),
					Kind:       "Certificate",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: certmanagerv1.CertificateSpec{
					IssuerRef:  cmmeta.ObjectReference{},
					SecretName: fmt.Sprintf("%s-tls", serviceName),
					DNSNames: []string{
						serviceName,
					},
					RenewBefore: &metav1.Duration{Duration: 240 * time.Hour},
				},
			},
		}
	}
}

func (b *DefaultCertBuilder) SetSecretName(secretName string) {
	b.cert.Spec.SecretName = secretName
}

func (b *DefaultCertBuilder) SetDNSNames(dnsNames []string) {
	b.cert.Spec.DNSNames = dnsNames
}

func (b *DefaultCertBuilder) SetIssuerRef(issuerRef cmmeta.ObjectReference) {
	b.cert.Spec.IssuerRef = issuerRef
}

func (b *DefaultCertBuilder) SetRenewBefore(duration metav1.Duration) {
	b.cert.Spec.RenewBefore = &duration
}

func (b *DefaultCertBuilder) Build() (certmanagerv1.Certificate, string, error) {
	marshalled, err := json.Marshal(b.cert)
	if err != nil {
		return certmanagerv1.Certificate{}, "", err
	}

	hash := fmt.Sprintf("%x", md5.Sum(marshalled))

	b.cert.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("cert-builder")
	logger.V(1).Info("Built Certificate", "name", b.cert.Name, "namespace", b.cert.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.cert, hash, nil
}

func CertBuilderFactory(builderType string, name string, namespace string, serviceName string, IssuerRef cmmeta.ObjectReference) (ICertificateBuilder, error) {
	switch builderType {
	case "Service":
		b := newDefaultCertBuilder(name, namespace, serviceName)
		b.SetIssuerRef(IssuerRef)
		return b, nil
	default:
		return nil, nil
	}
}
