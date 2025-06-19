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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	throwable "github.com/EdgeCDN-X/edgecdnx-controller/internal/throwable"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// LocationReconciler reconciles a Location object
type LocationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	ThrowerOptions
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/finalizers,verbs=update
// +kubebuilder:rbac:groups=argoproj.io,resources=applicationsets,verbs=get;list;watch;create;update;patch;delete
func (r *LocationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	location := &infrastructurev1alpha1.Location{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, location); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Location")
			return ctrl.Result{}, err
		}
		log.Info("Location resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	if location.Status != (infrastructurev1alpha1.LocationStatus{}) {
		// If status is healthy, check if location application set is rolled out
		if location.Status.Status == HealthStatusHealthy {
			appset := &argoprojv1alpha1.ApplicationSet{}

			// Do not sync status down the line. Not necessary
			resource := &infrastructurev1alpha1.Location{
				TypeMeta: location.TypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      location.Name,
					Namespace: r.InfrastructureTargetNamespace,
				},
				Spec: location.Spec,
			}

			locationHelmValues := throwable.ThrowerHelmValues{
				Resources: []any{resource},
			}

			desiredAppSpec, err := locationHelmValues.GetAppSetSpec(r.ThrowerChartRepository,
				r.ThrowerChartName,
				r.ThrowerChartVersion,
				r.InfrastructureApplicationSetNamespace,
				r.InfrastructureApplicationSetProject,
				r.InfrastructureTargetNamespace,
				fmt.Sprintf(`{{ metadata.labels.edgecdnx.com/location }}-location-%s`,
					location.Name))

			if err != nil {
				log.Error(err, "Failed to get ApplicationSet spec for Location")
				return ctrl.Result{}, err
			}

			md5Hash, err := locationHelmValues.GetMd5Hash()
			if err != nil {
				log.Error(err, "Failed to get MD5 hash for Helm values")
				return ctrl.Result{}, err
			}

			err = r.Get(ctx, types.NamespacedName{Namespace: r.InfrastructureApplicationSetNamespace, Name: location.Name}, appset)

			if err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("Creating ApplicationSet for Location", "name", location.Name)

					appset = &argoprojv1alpha1.ApplicationSet{
						ObjectMeta: metav1.ObjectMeta{
							Name:      location.Name,
							Namespace: r.InfrastructureApplicationSetNamespace,
							Annotations: map[string]string{
								ValuesHashAnnotation: md5Hash,
							},
						},
						Spec: desiredAppSpec,
					}

					err = controllerutil.SetControllerReference(location, appset, r.Scheme)
					if err != nil {
						log.Error(err, "Failed to set controller reference for ApplicationSet")
						return ctrl.Result{}, err
					}
					return ctrl.Result{}, r.Create(ctx, appset)
				} else {
					log.Error(err, "Failed to get ApplicationSet for Location")
					return ctrl.Result{}, err
				}
			}

			if !appset.DeletionTimestamp.IsZero() {
				log.Info("ApplicationSet for Location is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			currAppsetHash, ok := appset.ObjectMeta.Annotations[ValuesHashAnnotation]
			if !ok || currAppsetHash != md5Hash {
				log.Info("Updating ApplicationSet for Location")

				appset.Spec = desiredAppSpec
				appset.ObjectMeta.Annotations[ValuesHashAnnotation] = md5Hash

				return ctrl.Result{}, r.Update(ctx, appset)
			}

			return ctrl.Result{}, nil
		}
	} else {
		// Update status to healthy
		location.Status = infrastructurev1alpha1.LocationStatus{
			Status: HealthStatusHealthy,
		}
		return ctrl.Result{}, r.Status().Update(ctx, location)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LocationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Location{}).
		Owns(&argoprojv1alpha1.ApplicationSet{}).
		Complete(r)
}
