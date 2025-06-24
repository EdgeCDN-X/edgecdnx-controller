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
	"fmt"
	"maps"

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
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	ThrowerOptions
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
		Complete(r)
}
