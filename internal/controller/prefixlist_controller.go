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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// PrefixListReconciler reconciles a PrefixList object
type PrefixListReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists/finalizers,verbs=update

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *PrefixListReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	prefixList := &infrastructurev1alpha1.PrefixList{}
	if err := r.Get(ctx, req.NamespacedName, prefixList); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get PrefixList")
		return ctrl.Result{}, err
	}

	if prefixList.Status != (infrastructurev1alpha1.PrefixListStatus{}) {
		return ctrl.Result{}, nil
	}

	log.V(1).Info("PrefixList accepted. Setting status to healthy")
	prefixList.Status = infrastructurev1alpha1.PrefixListStatus{
		Status: HealthStatusHealthy,
	}
	return ctrl.Result{}, r.Status().Update(ctx, prefixList)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PrefixListReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.PrefixList{}).
		Complete(r)
}
