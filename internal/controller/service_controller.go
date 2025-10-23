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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	builder.ThrowerOptions
	ClusterIssuerName string
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services/finalizers,verbs=update
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

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
		serviceRawResource := &infrastructurev1alpha1.Service{
			TypeMeta: service.TypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name: service.Name,
			},
			Spec: service.Spec,
		}

		// ################ START - Certificate Issuance section
		certBuilder, err := builder.CertBuilderFactory("Service", service.Name, service.Namespace, service.Spec.Domain, cmmeta.ObjectReference{
			Name:  r.ClusterIssuerName,
			Kind:  "ClusterIssuer",
			Group: certmanagerv1.SchemeGroupVersion.Group,
		})

		if err != nil {
			log.Error(err, "Failed to create Certificate builder for Service")
			return ctrl.Result{}, err
		}

		desiredCert, hash, err := certBuilder.Build()
		if err != nil {
			log.Error(err, "Failed to get Certificate for Service")
			return ctrl.Result{}, err
		}

		currCert := &certmanagerv1.Certificate{}
		err = r.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, currCert)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating Certificate for Service", "name", service.Name)

				err = controllerutil.SetControllerReference(service, &desiredCert, r.Scheme)
				if err != nil {
					log.Error(err, "unable to set owner reference on Certificate for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, r.Create(ctx, &desiredCert)
			}

			log.Error(err, "unable to fetch Certificate for Service")
			return ctrl.Result{}, err
		} else {
			if !currCert.DeletionTimestamp.IsZero() {
				log.Info("Certificate for Service is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			// Update Controller Reference if missing (should not happen tho)
			if !controllerutil.HasControllerReference(currCert) {
				err := controllerutil.SetControllerReference(service, currCert, r.Scheme)
				if err != nil {
					log.Error(err, "unable to set owner reference on Certificate for Service", "Service", service.Name)
					return ctrl.Result{Requeue: true}, err
				}
				return ctrl.Result{}, r.Update(ctx, currCert)
			}

			// Update Certificate if spec changed
			currCertHash, ok := currCert.ObjectMeta.Annotations[builder.ValuesHashAnnotation]
			if !ok || currCertHash != hash {
				log.Info("Updating Certificate for Service", "name", service.Name)
				currCert.Spec = desiredCert.Spec
				currCert.ObjectMeta.Annotations = desiredCert.ObjectMeta.Annotations
				return ctrl.Result{}, r.Update(ctx, currCert)
			}
		}

		// Check if certificate is issuing
		for cc := range currCert.Status.Conditions {
			if currCert.Status.Conditions[cc].Type == certmanagerv1.CertificateConditionReady && currCert.Status.Conditions[cc].Status == cmmeta.ConditionTrue {
				secret := &corev1.Secret{}
				err = r.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: currCert.Spec.SecretName}, secret)
				if err != nil {
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					log.Error(err, "unable to fetch Certificate Secret for Service")
					return ctrl.Result{}, err
				}

				// Pass down the certificate to the resource
				serviceRawResource.Spec.Certificate = infrastructurev1alpha1.CertificateSpec{
					Key: base64.StdEncoding.EncodeToString(secret.Data["tls.key"]),
					Crt: base64.StdEncoding.EncodeToString(secret.Data["tls.crt"]),
				}
			}
		}

		// ################ START - Application Spec section
		appsetBuilder, err := builder.AppsetBuilderFactory("Service", service.Name, service.Namespace, r.ThrowerOptions)
		if err != nil {
			log.Error(err, "Failed to create ApplicationSet builder for Service")
			return ctrl.Result{}, err
		}

		serviceHelmValues := struct {
			Resources []any `json:"resources"`
		}{
			Resources: []any{serviceRawResource},
		}
		appsetBuilder.WithHelmValues(serviceHelmValues)

		desiredAppset, hash, err := appsetBuilder.Build()
		if err != nil {
			log.Error(err, "Failed to get ApplicationSet for Service")
			return ctrl.Result{}, err
		}

		currAppset := &argoprojv1alpha1.ApplicationSet{}
		err = r.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, currAppset)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating ApplicationSet for Service", "name", service.Name)

				err := controllerutil.SetControllerReference(service, &desiredAppset, r.Scheme)
				if err != nil {
					log.Error(err, "unable to set owner reference on ApplicationSet for Service", "Service", service.Name)
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, r.Create(ctx, &desiredAppset)
			} else {
				log.Error(err, "unable to fetch ApplicationSet for service")
				return ctrl.Result{}, err
			}
		} else {
			if !currAppset.DeletionTimestamp.IsZero() {
				log.Info("ApplicationSet for Service is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			currAppsetHash, ok := currAppset.ObjectMeta.Annotations[builder.ValuesHashAnnotation]
			if !ok || currAppsetHash != hash {
				log.Info("Updating ApplicationSet for Service", "name", service.Name)
				currAppset.Spec = desiredAppset.Spec
				currAppset.ObjectMeta.Annotations = desiredAppset.ObjectMeta.Annotations
				return ctrl.Result{}, r.Update(ctx, currAppset)
			}
		}

		// ################ END - Application Spec section

		if service.Status.Status != HealthStatusHealthy {
			service.Status = infrastructurev1alpha1.ServiceStatus{
				Status: HealthStatusHealthy,
			}
			return ctrl.Result{}, r.Status().Update(ctx, service)
		}

		return ctrl.Result{}, nil
	} else {
		// Set status to Pogressing if no satatus
		service.Status = infrastructurev1alpha1.ServiceStatus{
			Status: HealthStatusProgressing,
		}
		return ctrl.Result{}, r.Status().Update(ctx, service)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Service{}).
		Owns(&argoprojv1alpha1.ApplicationSet{}).
		Owns(&certmanagerv1.Certificate{}).
		Complete(r)
}
