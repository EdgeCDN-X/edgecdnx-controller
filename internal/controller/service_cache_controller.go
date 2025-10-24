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
	"context"
	"encoding/base64"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
			} else if !apierrors.IsNotFound(err) {
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

		// Manage certificate from Service TLS Secret
		secretName := service.Name + "-tls"
		secretBuilder := builder.NewSecretBuilder(secretName, service.Namespace)

		if service.Spec.Certificate.Crt != "" && service.Spec.Certificate.Key != "" {
			crt, err := base64.StdEncoding.DecodeString(service.Spec.Certificate.Crt)
			if err != nil {
				log.Error(err, "unable to decode certificate CRT for Secret")
				return ctrl.Result{}, err
			}
			key, err := base64.StdEncoding.DecodeString(service.Spec.Certificate.Key)
			if err != nil {
				log.Error(err, "unable to decode certificate KEY for Secret")
				return ctrl.Result{}, err
			}
			secretBuilder.WithDataEntry("tls.crt", crt)
			secretBuilder.WithDataEntry("tls.key", key)

			desiredSecret, hash, err := secretBuilder.Build()
			if err != nil {
				log.Error(err, "unable to build Secret spec")
				return ctrl.Result{}, err
			}

			curSecret := &v1.Secret{}
			err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: secretName}, curSecret)

			if err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("Secret not found, creating it", "Service", service.Name)

					err = controllerutil.SetControllerReference(service, &desiredSecret, r.Scheme)
					if err != nil {
						log.Error(err, "unable to set owner reference on Secret", "Service", service.Name)
						return ctrl.Result{}, err
					}
					return ctrl.Result{}, r.Create(ctx, &desiredSecret)
				} else {
					log.Error(err, "unable to fetch Secret", "Service", service.Name)
					return ctrl.Result{}, err
				}
			} else {
				curHash, ok := curSecret.Annotations[builder.ValuesHashAnnotation]
				if !ok || curHash != hash {
					log.Info("Updating Secret for Service", "Service", service.Name)
					curSecret.Data = desiredSecret.Data
					curSecret.SetAnnotations(desiredSecret.Annotations)
					return ctrl.Result{}, r.Update(ctx, curSecret)
				}
			}
		}

		// Ingress Resource
		ingressBuilder := builder.NewCacheIngressBuilder(service.Name, service.Namespace, builder.CacheIngressBuilderConfig{
			SecureUrlsEndpoint: r.SecureUrlsEndpoint,
		})

		ingressBuilder.WithService(*service)
		desiredIngress, hash, err := ingressBuilder.Build()

		currIngress := &networkingv1.Ingress{}
		err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: service.Name}, currIngress)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating Ingress for Service", "Service", service.Name)

				err = controllerutil.SetControllerReference(service, &desiredIngress, r.Scheme)

				if err != nil {
					log.Error(err, "unable to set owner reference on Ingress for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}

				err = r.Create(ctx, &desiredIngress)
				if err != nil {
					log.Error(err, "unable to create Ingress for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
			} else {
				log.Error(err, "unable to fetch Ingress", "Service", service.Name)
				return ctrl.Result{}, err
			}
		} else {
			curHash, ok := currIngress.Annotations[builder.ValuesHashAnnotation]

			if !ok || curHash != hash {
				currIngress.Spec = desiredIngress.Spec
				currIngress.Annotations = desiredIngress.Annotations
				return ctrl.Result{}, r.Update(ctx, currIngress)
			}
		}

		// Update Service Status to Healthy
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
