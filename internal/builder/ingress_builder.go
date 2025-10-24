package builder

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IIngressBuilder interface {
	WithLabel(key string, value string)
	WithAnnotation(key string, value string)
	WithIngressClass(ingressClass string)
	WithRules(rules []networkingv1.IngressRule)
	WithTls(tls []networkingv1.IngressTLS)
	Build() (networkingv1.Ingress, string, error)
}

type CacheIngressBuilder struct {
	ingress networkingv1.Ingress
	config  CacheIngressBuilderConfig
}

type CacheIngressBuilderConfig struct {
	SecureUrlsEndpoint string
}

func (b *CacheIngressBuilder) WithLabel(key string, value string) {
	if b.ingress.ObjectMeta.Labels == nil {
		b.ingress.ObjectMeta.Labels = map[string]string{}
	}
	b.ingress.ObjectMeta.Labels[key] = value
}

func (b *CacheIngressBuilder) WithAnnotation(key string, value string) {
	if b.ingress.ObjectMeta.Annotations == nil {
		b.ingress.ObjectMeta.Annotations = map[string]string{}
	}
	b.ingress.ObjectMeta.Annotations[key] = value
}

func (b *CacheIngressBuilder) WithIngressClass(ingressClass string) {
	b.ingress.Spec.IngressClassName = &ingressClass
}

func (b *CacheIngressBuilder) WithTls(tls []networkingv1.IngressTLS) {
	b.ingress.Spec.TLS = tls
}

func (b *CacheIngressBuilder) Build() (networkingv1.Ingress, string, error) {

	marshalled, err := json.Marshal(b.ingress)
	if err != nil {
		return networkingv1.Ingress{}, "", err
	}
	hash := fmt.Sprintf("%x", md5.Sum(marshalled))

	b.WithAnnotation(ValuesHashAnnotation, hash)

	return b.ingress, hash, nil
}

func (b *CacheIngressBuilder) WithRules(rules []networkingv1.IngressRule) {
	b.ingress.Spec.Rules = rules
}

func (b *CacheIngressBuilder) WithService(service infrastructurev1alpha1.Service) {
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific
	serviceName := strings.Replace(service.Name, ".", "-", -1)

	configSnippetTemplate := `
	{{- if and (eq .OriginType "static") (eq (index .StaticOrigins 0).Scheme "Https") }}
	proxy_ssl_name {{ (index .StaticOrigins 0).HostHeader }};
	proxy_ssl_server_name on;
	{{- end }}
	proxy_cache {{ .Cache }};
	proxy_cache_use_stale error timeout http_500 http_502 http_503 http_504;
	proxy_cache_background_update on;
	proxy_cache_revalidate on;
	proxy_cache_lock on;

	{{- with .CacheKeySpec.QueryParams }}
	access_by_lua_block {
		local h, err = ngx.req.get_uri_args()
		local allowed_headers = { {{ range . }}"{{ . }}",{{ end }} }

		for k, v in pairs(h) do
			local found = false
			for _, allowed in ipairs(allowed_headers) do
				if k == allowed then
					found = true
					break
				end
			end
			if not found then
				h[k] = nil
			end
		end

		ngx.req.set_uri_args(h)
	}
	{{- end }}

	proxy_cache_key $proxy_host$uri$is_args$args;
	add_header X-Cache-Key $proxy_host$uri$is_args$args;
	add_header X-EX-Status $upstream_cache_status;
	`

	tmpl, err := template.New("nginxconfigsnippet").Parse(configSnippetTemplate)
	if err != nil {
		return
	}

	var configSnippet bytes.Buffer
	err = tmpl.Execute(&configSnippet, service.Spec)
	if err != nil {
		return
	}

	b.WithIngressClass(service.Spec.Cache)

	b.WithAnnotation("nginx.ingress.kubernetes.io/configuration-snippet", configSnippet.String())
	b.WithAnnotation("nginx.ingress.kubernetes.io/server-snippet", `
location /.edgecdnx/healthz {
	return 200 "OK";
}
	`)
	b.WithAnnotation("nginx.ingress.kubernetes.io/enable-cors", "true")
	b.WithAnnotation("nginx.ingress.kubernetes.io/cors-allow-methods", "GET,OPTIONS,HEAD")
	b.WithAnnotation("nginx.ingress.kubernetes.io/cors-allow-origin", "*")
	b.WithAnnotation("nginx.ingress.kubernetes.io/cors-allow-credentials", "true")

	if service.Spec.Certificate.Crt != "" && service.Spec.Certificate.Key != "" {
		b.WithTls([]networkingv1.IngressTLS{
			{
				Hosts:      []string{service.Spec.Domain},
				SecretName: service.Name + "-tls",
			},
		})
	}

	if len(service.Spec.SecureKeys) > 0 {
		b.WithAnnotation("nginx.ingress.kubernetes.io/auth-url", b.config.SecureUrlsEndpoint)
	}

	if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeStatic {
		b.WithAnnotation("nginx.ingress.kubernetes.io/backend-protocol", strings.ToUpper(service.Spec.StaticOrigins[0].Scheme))
		b.WithAnnotation("nginx.ingress.kubernetes.io/upstream-vhost", service.Spec.StaticOrigins[0].HostHeader)

		b.WithRules([]networkingv1.IngressRule{
			{
				Host: service.Spec.Domain,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathTypeImplementationSpecific,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: serviceName,
										Port: networkingv1.ServiceBackendPort{
											Number: int32(service.Spec.StaticOrigins[0].Port),
										},
									},
								},
							},
						},
					},
				},
			},
		})
	}

	if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeS3 {
		b.WithRules([]networkingv1.IngressRule{
			{
				Host: service.Spec.Domain,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathTypeImplementationSpecific,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: serviceName,
										Port: networkingv1.ServiceBackendPort{
											Number: int32(80),
										},
									},
								},
							},
						},
					},
				},
			},
		})
	}
}

func NewCacheIngressBuilder(name string, namespace string, config CacheIngressBuilderConfig) *CacheIngressBuilder {
	return &CacheIngressBuilder{
		ingress: networkingv1.Ingress{
			TypeMeta: metav1.TypeMeta{
				APIVersion: networkingv1.SchemeGroupVersion.String(),
				Kind:       "Ingress",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{},
			},
		},
		config: config,
	}
}
