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
	networkingv1 "k8s.io/api/networking/v1"

	v1 "k8s.io/api/core/v1"
)

// ServiceCacheReconciler reconciles a Service object
type ServiceCacheReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ServiceCacheReconciler) getIngressCache(service *infrastructurev1alpha1.Service) (networkingv1.IngressSpec, map[string]string, string, error) {
	extServiceName := strings.Replace(service.Name, ".", "-", -1)

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
							Paths: []networkingv1.HTTPIngressPath{
								{
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
								},
							},
						},
					},
				},
			},
		},
		Annotations: map[string]string{
			"nginx.ingress.kubernetes.io/backend-protocol":      strings.ToUpper(service.Spec.StaticOrigins[0].Scheme),
			"nginx.ingress.kubernetes.io/upstream-vhost":        service.Spec.StaticOrigins[0].HostHeader,
			"nginx.ingress.kubernetes.io/configuration-snippet": configSnippet.String(),
		},
	}

	hashable, err := json.Marshal(marshable)
	if err != nil {
		return networkingv1.IngressSpec{}, make(map[string]string), "", fmt.Errorf("failed to marshal values object: %w", err)
	}
	return marshable.Spec, marshable.Annotations, fmt.Sprintf("%x", md5.Sum(hashable)), nil
}

func (r *ServiceCacheReconciler) getServiceCache(service *infrastructurev1alpha1.Service) (v1.ServiceSpec, map[string]string, string, error) {
	marshable := struct {
		Spec        v1.ServiceSpec    `json:"spec"`
		Annotations map[string]string `json:"annotations"`
	}{
		Spec: v1.ServiceSpec{
			ExternalName: service.Spec.StaticOrigins[0].Upstream,
			Type:         v1.ServiceTypeExternalName,
		},
		Annotations: map[string]string{},
	}

	hashable, err := json.Marshal(marshable)
	if err != nil {
		return v1.ServiceSpec{}, make(map[string]string), "", fmt.Errorf("failed to marshal values object: %w", err)
	}
	return marshable.Spec, marshable.Annotations, fmt.Sprintf("%x", md5.Sum(hashable)), nil
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
func (r *ServiceCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.Info("Reconciling ServiceCache", "Service", req.Name)

	service := &infrastructurev1alpha1.Service{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Service")
			return ctrl.Result{}, err
		}
		log.Info("Service resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	if service.Status != (infrastructurev1alpha1.ServiceStatus{}) {

		// Find ExternalName Service
		extService := &v1.Service{}
		extServiceName := strings.Replace(service.Name, ".", "-", -1)

		err := r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: extServiceName}, extService)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("ExternalName Service not found, creating it", "Service", extServiceName)

				spec, annotations, hash, err := r.getServiceCache(service)

				if err != nil {
					return ctrl.Result{}, err
				}

				objAnnotations := map[string]string{
					ValuesHashAnnotation: hash,
				}

				maps.Copy(objAnnotations, annotations)

				// StaticOrigin use case
				extService = &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:        extServiceName,
						Namespace:   service.Namespace,
						Annotations: objAnnotations,
					},
					Spec: spec,
				}

				controllerutil.SetControllerReference(service, extService, r.Scheme)
				return ctrl.Result{}, r.Create(ctx, extService)
			} else {
				log.Error(err, "unable to fetch Service", "Service", extServiceName)
				return ctrl.Result{Requeue: true}, err
			}
		} else {
			spec, annotations, hash, err := r.getServiceCache(service)

			if err != nil {
				return ctrl.Result{}, err
			}

			curHash, ok := extService.Annotations[ValuesHashAnnotation]
			if !ok || curHash != hash {
				extService.Spec = spec
				extService.Annotations = annotations
				extService.Annotations[ValuesHashAnnotation] = hash
				return ctrl.Result{}, r.Update(ctx, extService)
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
					ValuesHashAnnotation: hash,
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
				controllerutil.SetControllerReference(service, ingress, r.Scheme)
				err = r.Create(ctx, ingress)
				if err != nil {
					log.Error(err, "unable to create Ingress for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
			} else {
				log.Error(err, "unable to fetch Ingress", "Service", service.Name)
				return ctrl.Result{Requeue: true}, err
			}
		} else {
			spec, annotations, hash, err := r.getIngressCache(service)

			if err != nil {
				return ctrl.Result{}, err
			}

			curHash, ok := ingress.Annotations[ValuesHashAnnotation]

			if !ok || curHash != hash {
				ingress.Spec = spec
				ingress.Annotations = annotations
				ingress.Annotations[ValuesHashAnnotation] = hash
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
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
