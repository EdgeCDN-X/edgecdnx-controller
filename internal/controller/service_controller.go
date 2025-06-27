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
	"crypto/md5"
	"encoding/json"
	"fmt"
	"maps"
	"time"

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
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/throwable"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	ThrowerOptions
	ClusterIssuerName string
}

func (r *ServiceReconciler) GetCertificateSpec(service *infrastructurev1alpha1.Service) (certmanagerv1.CertificateSpec, map[string]string, string, error) {

	spec := certmanagerv1.CertificateSpec{
		SecretName:  fmt.Sprintf("%s-tls", service.Name),
		RenewBefore: &metav1.Duration{Duration: 240 * time.Hour},
		IssuerRef: cmmeta.ObjectReference{
			Name:  r.ClusterIssuerName,
			Kind:  "ClusterIssuer",
			Group: certmanagerv1.SchemeGroupVersion.Group,
		},
		DNSNames: []string{
			service.Spec.Domain,
		},
	}

	marshable := struct {
		Spec        certmanagerv1.CertificateSpec `json:"spec"`
		Annotations map[string]string             `json:"annotations"`
	}{
		Spec:        spec,
		Annotations: map[string]string{},
	}

	hashable, err := json.Marshal(marshable)
	if err != nil {
		return certmanagerv1.CertificateSpec{}, nil, "", fmt.Errorf("failed to marshal certificate spec: %w", err)
	}

	return marshable.Spec, marshable.Annotations, fmt.Sprintf("%x", md5.Sum(hashable)), nil

}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services/finalizers,verbs=update
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Service object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
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
		appset := &argoprojv1alpha1.ApplicationSet{}

		resource := &infrastructurev1alpha1.Service{
			TypeMeta: service.TypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Name,
				Namespace: r.InfrastructureTargetNamespace,
			},
			Spec: service.Spec,
		}
		serviceHelmValues := throwable.ThrowerHelmValues{
			Resources: []any{resource},
		}

		// ################ START - Application Spec section
		spec, annotations, hash, err := serviceHelmValues.GetAppSetSpec(
			throwable.AppsetSpecOptions{
				ChartRepository: r.ThrowerChartRepository,
				Chart:           r.ThrowerChartName,
				ChartVersion:    r.ThrowerChartVersion,
				Project:         r.InfrastructureApplicationSetProject,
				TargetNamespace: r.InfrastructureTargetNamespace,
				Name:            fmt.Sprintf(`{{ metadata.labels.edgecdnx.com/location }}-service-%s`, service.Name),
				// Roll out for both routing and caching
				LabelMatch: [][]metav1.LabelSelectorRequirement{
					{
						{
							Key:      "edgecdnx.com/routing",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"true", "yes"},
						},
					},
					{
						{
							Key:      "edgecdnx.com/caching",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"true", "yes"},
						},
					},
				},
			},
		)

		if err != nil {
			log.Error(err, "Failed to get ApplicationSet spec for Service")
			return ctrl.Result{}, err
		}

		err = r.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, appset)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating ApplicationSet for Service", "name", service.Name)

				objAnnotations := map[string]string{
					ValuesHashAnnotation: hash,
				}

				maps.Copy(objAnnotations, annotations)
				appset = &argoprojv1alpha1.ApplicationSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:        service.Name,
						Namespace:   service.Namespace,
						Annotations: objAnnotations,
					},
					Spec: spec,
				}

				controllerutil.SetControllerReference(service, appset, r.Scheme)
				return ctrl.Result{}, r.Create(ctx, appset)
			} else {
				log.Error(err, "unable to fetch ApplicationSet for service")
				return ctrl.Result{}, err
			}
		} else {
			if !appset.DeletionTimestamp.IsZero() {
				log.Info("ApplicationSet for Service is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			currAppsetHash, ok := appset.ObjectMeta.Annotations[ValuesHashAnnotation]
			if !ok || currAppsetHash != hash {
				log.Info("Updating ApplicationSet for Service", "name", service.Name)
				appset.Spec = spec
				maps.Copy(appset.ObjectMeta.Annotations, annotations)
				appset.ObjectMeta.Annotations[ValuesHashAnnotation] = hash
				return ctrl.Result{}, r.Update(ctx, appset)
			}
		}

		// ################ END - Application Spec section

		// ################ START - Certificate Issuance section

		certSpec, annotations, hash, err := r.GetCertificateSpec(service)
		if err != nil {
			log.Error(err, "Failed to get Certificate spec for Service")
			return ctrl.Result{}, err
		}

		cert := &certmanagerv1.Certificate{}
		err = r.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, cert)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating Certificate for Service", "name", service.Name)

				objAnnotations := map[string]string{
					ValuesHashAnnotation: hash,
				}

				cert = &certmanagerv1.Certificate{
					ObjectMeta: metav1.ObjectMeta{
						Name:        service.Name,
						Namespace:   service.Namespace,
						Annotations: objAnnotations,
					},
					Spec: certSpec,
				}

				controllerutil.SetControllerReference(service, cert, r.Scheme)
				return ctrl.Result{}, r.Create(ctx, cert)
			}

			log.Error(err, "unable to fetch Certificate for Service")
			return ctrl.Result{}, err
		} else {
			if !cert.DeletionTimestamp.IsZero() {
				log.Info("Certificate for Service is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			currCertHash, ok := cert.ObjectMeta.Annotations[ValuesHashAnnotation]
			if !ok || currCertHash != hash {
				log.Info("Updating Certificate for Service", "name", service.Name)
				cert.Spec = certSpec
				cert.ObjectMeta.Annotations = annotations
				cert.ObjectMeta.Annotations[ValuesHashAnnotation] = hash
				return ctrl.Result{}, r.Update(ctx, cert)
			}
		}

		// TODO, this should run before the ApplicationSet is created.
		// Check if certificate is issuing
		for cc := range cert.Status.Conditions {
			if cert.Status.Conditions[cc].Type == certmanagerv1.CertificateConditionReady && cert.Status.Conditions[cc].Status == cmmeta.ConditionTrue {
				secret := &corev1.Secret{}
				err = r.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: cert.Spec.SecretName}, secret)
				if err != nil {
					if apierrors.IsNotFound(err) {
						return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
					}
					log.Error(err, "unable to fetch Certificate Secret for Service")
					return ctrl.Result{}, err
				}
			}

			if cert.Status.Conditions[cc].Type == certmanagerv1.CertificateConditionIssuing && cert.Status.Conditions[cc].Status == cmmeta.ConditionTrue {
				log.Info("Certificate is still issuing for Service")
				return ctrl.Result{}, nil
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
