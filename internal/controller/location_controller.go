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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller.git/api/v1alpha1"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// LocationReconciler reconciles a Location object
type LocationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type LocationHelmValues struct {
	Name     string                              `json:"name,omitempty"`
	Location infrastructurev1alpha1.LocationSpec `json:"location"`
}

const HealthStatusHealthy = "Healthy"
const ValuesHashAnnotation = "edgecdnx.edgedcnx.com/values-hash"

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/finalizers,verbs=update
// +kubebuilder:rbac:groups=argoproj.io,resources=applicationsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Location object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *LocationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	location := &infrastructurev1alpha1.Location{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, location); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Location")
			return ctrl.Result{}, err
		}
		// Request object not found, could have been deleted after reconcile request.
		log.Info("Location resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	if location.Status != (infrastructurev1alpha1.LocationStatus{}) {
		// If status is healthy, check if location application set is rolled out
		if location.Status.Status == "Healthy" {
			appset := &argoprojv1alpha1.ApplicationSet{}

			locationHelmValues := LocationHelmValues{
				Name:     location.Name,
				Location: location.Spec,
			}

			valuesObject, err := json.Marshal(locationHelmValues)

			if err != nil {
				log.Error(err, "Failed to marshal location spec")
				return ctrl.Result{}, err
			}
			valuesHash := md5.Sum(valuesObject)
			desiredAppSpec := argoprojv1alpha1.ApplicationSetSpec{
				Generators: []argoprojv1alpha1.ApplicationSetGenerator{
					{
						Clusters: &argoprojv1alpha1.ClusterGenerator{
							Selector: metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "edgecdnx.com/routing",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"true", "yes"},
									},
								},
							},
							Values: map[string]string{
								// TODO make these configurable
								"chartRepository": "https://edgecdn-x.github.io/helm-charts",
								"chart":           "infrastructure-location",
								"chartVersion":    "0.1.1",
							},
						},
					},
				},
				Template: argoprojv1alpha1.ApplicationSetTemplate{
					ApplicationSetTemplateMeta: argoprojv1alpha1.ApplicationSetTemplateMeta{
						Name:      fmt.Sprintf(`location-%s-{{ metadata.labels.edgecdnx.com/location }}`, location.Name),
						Namespace: "argocd",
					},
					Spec: argoprojv1alpha1.ApplicationSpec{
						Project: "default",
						Destination: argoprojv1alpha1.ApplicationDestination{
							Server:    "{{ server }}",
							Namespace: "edgecdnx-routing",
						},
						Sources: []argoprojv1alpha1.ApplicationSource{
							{
								Chart:          "{{ values.chart }}",
								RepoURL:        "{{ values.chartRepository }}",
								TargetRevision: "{{ values.chartVersion }}",
								Helm: &argoprojv1alpha1.ApplicationSourceHelm{
									ReleaseName: "{{ name }}",
									ValuesObject: &runtime.RawExtension{
										Raw: valuesObject,
									},
								},
							},
						},
					},
				},
			}

			err = r.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: location.Name}, appset)

			if err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("Creating ApplicationSet for Location", "name", location.Name)

					appset = &argoprojv1alpha1.ApplicationSet{
						ObjectMeta: metav1.ObjectMeta{
							Name:      location.Name,
							Namespace: "argocd",
							Annotations: map[string]string{
								ValuesHashAnnotation: fmt.Sprintf("%x", valuesHash),
							},
						},
						Spec: desiredAppSpec,
					}

					controllerutil.SetControllerReference(location, appset, r.Scheme)
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

			//TODO
			// currAppsetHash, ok := appset.ObjectMeta.Annotations[ValuesHashAnnotation]
			// if !ok || currAppsetHash != fmt.Sprintf("%x", valuesHash) {
			// 	log.Info("Updating ApplicationSet for Location")

			// 	appset.Spec = desiredAppSpec
			// 	appset.ObjectMeta.Annotations[ValuesHashAnnotation] = fmt.Sprintf("%x", valuesHash)

			// 	return ctrl.Result{}, r.Update(ctx, appset)
			// }

			return ctrl.Result{}, nil
		}
	} else {
		// Update status to healthy
		location.Status = infrastructurev1alpha1.LocationStatus{
			Status: "Healthy",
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
