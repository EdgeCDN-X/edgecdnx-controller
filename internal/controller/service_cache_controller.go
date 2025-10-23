/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"text/template"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	networkingv1 "k8s.io/api/networking/v1"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

// ServiceCacheReconciler reconciles a Service object
type ServiceCacheReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	SecureUrlsEndpoint string
}

func (r *ServiceCacheReconciler) getIngressCache(service *infrastructurev1alpha1.Service) (networkingv1.IngressSpec, map[string]string, string, error) {
	extServiceName := strings.Replace(service.Name, ".", "-", -1)
	s3GatewayServiceName := extServiceName + "-s3-gateway"

	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific

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
		return networkingv1.IngressSpec{}, make(map[string]string), "", fmt.Errorf("failed to parse template: %w", err)
	}

	var configSnippet bytes.Buffer
	err = tmpl.Execute(&configSnippet, service.Spec)
	if err != nil {
		return networkingv1.IngressSpec{}, make(map[string]string), "", fmt.Errorf("failed to execute template: %w", err)
	}

	marshable := struct {
		Spec        networkingv1.IngressSpec `json:"spec"`
		Annotations map[string]string        `json:"annotations"`
	}{
		Spec: networkingv1.IngressSpec{
			IngressClassName: &service.Spec.Cache,
			Rules: []networkingv1.IngressRule{
				{
					Host: service.Spec.Domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{},
						},
					},
				},
			},
		},
		Annotations: map[string]string{
			"nginx.ingress.kubernetes.io/configuration-snippet": configSnippet.String(),
			"nginx.ingress.kubernetes.io/server-snippet": `
location /.edgecdnx/healthz {
	return 200 "OK";
}
			`,
			"nginx.ingress.kubernetes.io/enable-cors":            "true",
			"nginx.ingress.kubernetes.io/cors-allow-methods":     "GET,OPTIONS,HEAD",
			"nginx.ingress.kubernetes.io/cors-allow-origin":      "*",
			"nginx.ingress.kubernetes.io/cors-allow-credentials": "true",
		},
	}

	if service.Spec.Certificate.Key != "" && service.Spec.Certificate.Crt != "" {
		marshable.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{service.Spec.Domain},
				SecretName: service.Name + "-tls",
			},
		}
	}

	if len(service.Spec.SecureKeys) > 0 {
		marshable.Annotations["nginx.ingress.kubernetes.io/auth-url"] = r.SecureUrlsEndpoint
	}

	if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeStatic {
		marshable.Annotations["nginx.ingress.kubernetes.io/backend-protocol"] = strings.ToUpper(service.Spec.StaticOrigins[0].Scheme)
		marshable.Annotations["nginx.ingress.kubernetes.io/upstream-vhost"] = service.Spec.StaticOrigins[0].HostHeader
		marshable.Spec.Rules[0].IngressRuleValue.HTTP.Paths = append(marshable.Spec.Rules[0].IngressRuleValue.HTTP.Paths, networkingv1.HTTPIngressPath{
			Path:     "/",
			PathType: &pathTypeImplementationSpecific,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: extServiceName,
					Port: networkingv1.ServiceBackendPort{
						Number: int32(service.Spec.StaticOrigins[0].Port),
					},
				},
			},
		})
	}

	if service.Spec.OriginType == "s3" {
		marshable.Spec.Rules[0].IngressRuleValue.HTTP.Paths = append(marshable.Spec.Rules[0].IngressRuleValue.HTTP.Paths, networkingv1.HTTPIngressPath{
			Path:     "/",
			PathType: &pathTypeImplementationSpecific,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: s3GatewayServiceName,
					Port: networkingv1.ServiceBackendPort{
						Number: int32(80),
					},
				},
			},
		})
	}

	hashable, err := json.Marshal(marshable)
	if err != nil {
		return networkingv1.IngressSpec{}, make(map[string]string), "", fmt.Errorf("failed to marshal values object: %w", err)
	}
	return marshable.Spec, marshable.Annotations, fmt.Sprintf("%x", md5.Sum(hashable)), nil
}

func (r *ServiceCacheReconciler) getSecretCache(service *infrastructurev1alpha1.Service) (v1.Secret, map[string]string, string, error) {
	secretName := service.Name + "-tls"

	marshable := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   service.Namespace,
			Annotations: map[string]string{},
		},
		Type: v1.SecretTypeTLS,
		Data: map[string][]byte{},
	}

	if service.Spec.Certificate.Key != "" {
		tlsKey, err := base64.StdEncoding.DecodeString(service.Spec.Certificate.Key)
		if err != nil {
			return v1.Secret{}, make(map[string]string), "", fmt.Errorf("failed to decode TLS key")
		}

		marshable.Data["tls.key"] = tlsKey
	} else {
		marshable.Data["tls.key"] = []byte{}
	}

	if service.Spec.Certificate.Crt != "" {
		tlsCrt, err := base64.StdEncoding.DecodeString(service.Spec.Certificate.Crt)
		if err != nil {
			return v1.Secret{}, make(map[string]string), "", fmt.Errorf("failed to decode CRT")
		}
		marshable.Data["tls.crt"] = tlsCrt
	} else {
		marshable.Data["tls.crt"] = []byte{}
	}

	hashable, err := json.Marshal(marshable)
	if err != nil {
		return v1.Secret{}, make(map[string]string), "", fmt.Errorf("failed to marshal values object: %w", err)
	}
	return marshable, marshable.Annotations, fmt.Sprintf("%x", md5.Sum(hashable)), nil
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Service object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
// nolint: gocyclo, vet
func (r *ServiceCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.Info("Reconciling CacheService", "Service", req.Name)

	service := &infrastructurev1alpha1.Service{}

	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Service")
			return ctrl.Result{}, err
		}
		log.Info("Service resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	if !service.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Service is being deleted, ignoring reconcile loop")
		return ctrl.Result{}, nil
	}

	if service.Status != (infrastructurev1alpha1.ServiceStatus{}) {
		// Service ->
		// On Static upstream => ExternalName Service
		// On S3 upstream => S3 Gateway Service + deployment

		// Service
		serviceName := strings.Replace(service.Name, ".", "-", -1)
		serviceBuilder, err := builder.CoreV1ServiceBuilderFactory("default", serviceName, service.Namespace)

		if err != nil {
			log.Error(err, "unable to create ServiceBuilder", "Service", service.Name)
			return ctrl.Result{}, err
		}

		if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeStatic {
			serviceBuilder.WithUpstream(service.Spec.StaticOrigins[0].Upstream)
		}

		if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeS3 {
			serviceBuilder.WithS3Gateway(service.Spec.Domain)
		}

		desiredService, hash, err := serviceBuilder.Build()
		if err != nil {
			log.Error(err, "unable to build Service spec", "Service", service.Name)
			return ctrl.Result{}, err
		}

		log.Info("Checking V1Service for Service", "Service", service.Name)
		curService := &v1.Service{}
		err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: serviceName}, curService)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Service not found, creating it", "Service", service.Name)

				err = controllerutil.SetControllerReference(service, &desiredService, r.Scheme)
				if err != nil {
					log.Error(err, "unable to set owner reference on Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, r.Create(ctx, &desiredService)
			} else {
				log.Error(err, "unable to fetch Service", "Service", service.Name)
				return ctrl.Result{}, err
			}
		} else {
			curHash, ok := curService.Annotations[builder.ValuesHashAnnotation]
			if !ok || curHash != hash {
				log.Info("Updating Service for Service", "Service", service.Name)
				curService.Spec = desiredService.Spec
				curService.SetAnnotations(desiredService.Annotations)
				return ctrl.Result{}, r.Update(ctx, curService)
			}
		}

		// S3Gateway Deployment
		deploymentName := strings.Replace(service.Name, ".", "-", -1) + "-s3gateway"

		// If OriginType is static, delete deployment if exists
		if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeStatic {
			// If not S3 origin type, ensure deployment is deleted if exists
			deployment := &appsv1.Deployment{}
			err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: deploymentName}, deployment)
			if err == nil {
				log.Info("Deleting S3 Gateway Deployment for Service", "Service", service.Name)
				return ctrl.Result{}, r.Delete(ctx, deployment)
			} else if apierrors.IsNotFound(err) {
				// Deployment not found, nothing to do
				return ctrl.Result{}, nil
			} else {
				log.Error(err, "unable to fetch Deployment", "Service", service.Name)
				return ctrl.Result{}, err
			}
		}

		// If OriginType is S3, ensure deployment exists
		if service.Spec.OriginType == infrastructurev1alpha1.OriginTypeS3 {
			deploymentBuilder, err := builder.DeploymentBuilderFactory("s3-gateway", deploymentName, service.Namespace)
			if err != nil {
				log.Error(err, "unable to create DeploymentBuilder", "Service", service.Name)
				return ctrl.Result{}, err
			}

			deploymentBuilder.WithService(service)

			desiredDeployment, hash, err := deploymentBuilder.Build()

			if err != nil {
				log.Error(err, "unable to build Deployment spec", "Service", service.Name)
				return ctrl.Result{}, err
			}

			log.Info("Checking Deployment for Service", "Service", service.Name)
			curDeployment := &appsv1.Deployment{}
			err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: deploymentName}, curDeployment)

			if err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("Deployment not found, creating it", "Service", service.Name)

					err = controllerutil.SetControllerReference(service, &desiredDeployment, r.Scheme)
					if err != nil {
						log.Error(err, "unable to set owner reference on Deployment", "Service", service.Name)
						return ctrl.Result{}, err
					}
					return ctrl.Result{}, r.Create(ctx, &desiredDeployment)
				} else {
					log.Error(err, "unable to fetch Deployment", "Service", service.Name)
					return ctrl.Result{}, err
				}
			} else {
				curHash, ok := curDeployment.Annotations[builder.ValuesHashAnnotation]
				if !ok || curHash != hash {
					log.Info("Updating Deployment for Service", "Service", service.Name)
					curDeployment.Spec = desiredDeployment.Spec
					curDeployment.SetAnnotations(desiredDeployment.Annotations)
					return ctrl.Result{}, r.Update(ctx, curDeployment)
				}
			}
		}

		// Find Certificate Secret Resource
		secret := &v1.Secret{}
		secretName := service.Name + "-tls"
		err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: secretName}, secret)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Secret not found for service")

				nSecret, annotations, hash, err := r.getSecretCache(service)
				if err != nil {
					return ctrl.Result{}, err
				}

				objAnnotations := map[string]string{
					builder.ValuesHashAnnotation: hash,
				}

				maps.Copy(objAnnotations, annotations)
				nSecret.Annotations = objAnnotations

				err = controllerutil.SetControllerReference(service, &nSecret, r.Scheme)
				if err != nil {
					log.Error(err, "unable to set owner reference on Secret for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
				err = r.Create(ctx, &nSecret)
				if err != nil {
					log.Error(err, "unable to create Secret for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
			} else {
				log.Error(err, "unable to fetch Secret for Service", "Service", service.Name)
				return ctrl.Result{}, err
			}
		} else {
			nSecret, annotations, hash, err := r.getSecretCache(service)

			if err != nil {
				return ctrl.Result{}, err
			}

			curHash, ok := secret.Annotations[builder.ValuesHashAnnotation]
			if !ok || curHash != hash {

				log.Info("Updating Secret for Service", "Service", service.Name)

				secret.Annotations = annotations
				secret.Data = nSecret.Data
				return ctrl.Result{}, r.Update(ctx, secret)
			}
		}

		// Find ingress resource
		ingress := &networkingv1.Ingress{}
		err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: service.Name}, ingress)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating Ingress for Service", "Service", service.Name)

				spec, annotations, hash, err := r.getIngressCache(service)

				if err != nil {
					return ctrl.Result{}, err
				}

				objAnnotations := map[string]string{
					builder.ValuesHashAnnotation: hash,
				}

				maps.Copy(objAnnotations, annotations)

				ingress = &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:        service.Name,
						Namespace:   service.Namespace,
						Annotations: objAnnotations,
					},
					Spec: spec,
				}
				err = controllerutil.SetControllerReference(service, ingress, r.Scheme)
				if err != nil {
					log.Error(err, "unable to set owner reference on Ingress for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
				err = r.Create(ctx, ingress)
				if err != nil {
					log.Error(err, "unable to create Ingress for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
			} else {
				log.Error(err, "unable to fetch Ingress", "Service", service.Name)
				return ctrl.Result{}, err
			}
		} else {
			spec, annotations, hash, err := r.getIngressCache(service)

			if err != nil {
				return ctrl.Result{}, err
			}

			curHash, ok := ingress.Annotations[builder.ValuesHashAnnotation]

			if !ok || curHash != hash {
				ingress.Spec = spec
				ingress.Annotations = annotations
				ingress.Annotations[builder.ValuesHashAnnotation] = hash
				return ctrl.Result{}, r.Update(ctx, ingress)
			}
		}

		service.Status = infrastructurev1alpha1.ServiceStatus{
			Status: HealthStatusHealthy,
		}

		return ctrl.Result{}, r.Status().Update(ctx, service)
	} else {
		service.Status = infrastructurev1alpha1.ServiceStatus{
			Status: HealthStatusProgressing,
		}
		return ctrl.Result{}, r.Status().Update(ctx, service)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Service{}).
		Owns(&v1.Service{}).
		Owns(&v1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
