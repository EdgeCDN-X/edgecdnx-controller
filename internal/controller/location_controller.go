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
)

// LocationReconciler reconciles a Location object
type LocationReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ThrowerOptions builder.ThrowerOptions
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/finalizers,verbs=update
// +kubebuilder:rbac:groups=argoproj.io,resources=applicationsets,verbs=get;list;watch;create;update;patch;delete
// Reconcile is part of the main kubernetes reconciliation loop which aims to

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

	if location.Status.Status == HealthStatusProgressing || location.Status.Status == HealthStatusHealthy {
		// Strip off resource from Namespace. Applicationset Will take care of it.
		resource := &infrastructurev1alpha1.Location{
			TypeMeta: location.TypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name: location.Name,
			},
			Spec: location.Spec,
		}

		locationHelmValues := struct {
			Resources []any `json:"resources"`
		}{
			Resources: []any{resource},
		}

		appsetBuilder, err := builder.AppsetBuilderFactory("Location", location.Name, location.Namespace, r.ThrowerOptions)
		if err != nil {
			log.Error(err, "Failed to create ApplicationSet builder for Location")
			return ctrl.Result{}, err
		}
		appsetBuilder.WithHelmValues(locationHelmValues)
		desiredAppset, hash, err := appsetBuilder.Build()

		if err != nil {
			log.Error(err, "Failed to get ApplicationSet for Location")
			return ctrl.Result{}, err
		}

		currAppset := &argoprojv1alpha1.ApplicationSet{}
		err = r.Get(ctx, types.NamespacedName{Namespace: location.Namespace, Name: location.Name}, currAppset)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating ApplicationSet for Location", "hash", hash)

				err := controllerutil.SetControllerReference(location, &desiredAppset, r.Scheme)
				if err != nil {
					log.Error(err, "Failed to set controller reference on ApplicationSet")
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, r.Create(ctx, &desiredAppset)
			} else {
				log.Error(err, "Failed to get ApplicationSet for Location")
				return ctrl.Result{}, err
			}
		} else {
			if !currAppset.DeletionTimestamp.IsZero() {
				log.Info("ApplicationSet for Location is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			currAppsetHash, ok := currAppset.ObjectMeta.Annotations[builder.ValuesHashAnnotation]
			if !ok || currAppsetHash != hash {
				log.Info("Updating ApplicationSet for Location", "old-hash", currAppsetHash, "new-hash", hash)
				currAppset.Spec = desiredAppset.Spec
				currAppset.ObjectMeta.Annotations = desiredAppset.ObjectMeta.Annotations
				return ctrl.Result{}, r.Update(ctx, currAppset)
			}
		}

		if location.Status.Status != HealthStatusHealthy {
			location.Status = infrastructurev1alpha1.LocationStatus{
				Status: HealthStatusHealthy,
			}
			return ctrl.Result{}, r.Status().Update(ctx, location)
		}

		return ctrl.Result{}, nil

	} else {
		// Update status to healthy
		location.Status = infrastructurev1alpha1.LocationStatus{
			Status:     HealthStatusProgressing,
			NodeStatus: make(map[string]infrastructurev1alpha1.NodeInstanceStatus),
		}
		return ctrl.Result{}, r.Status().Update(ctx, location)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *LocationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Location{}).
		Owns(&argoprojv1alpha1.ApplicationSet{}).
		Complete(r)
}
