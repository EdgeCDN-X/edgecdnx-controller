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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// LocationRoutingReconciler reconciles a Location object
type LocationRoutingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Location object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *LocationRoutingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	location := &infrastructurev1alpha1.Location{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, location); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	switch location.Status.Status {
	case HealthStatusHealthy:
		// No action needed
		return ctrl.Result{}, nil
	case HealthStatusProgressing:
		// Update to Progressing
		location.Status = infrastructurev1alpha1.LocationStatus{
			Status:     HealthStatusHealthy,
			NodeStatus: location.Status.NodeStatus,
		}
		return ctrl.Result{}, r.Status().Update(ctx, location)
	default:
		location.Status = infrastructurev1alpha1.LocationStatus{
			Status:     HealthStatusProgressing,
			NodeStatus: make(map[string]infrastructurev1alpha1.NodeInstanceStatus),
		}
		return ctrl.Result{}, r.Status().Update(ctx, location)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *LocationRoutingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Location{}).
		Complete(r)
}
