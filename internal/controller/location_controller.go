package controller

import (
	"context"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type LocationReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/status,verbs=get;update;patch

func (r *LocationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	location := &infrastructurev1alpha1.Location{}
	if err := r.Get(ctx, req.NamespacedName, location); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !location.DeletionTimestamp.IsZero() || location.Status.Status == HealthStatusHealthy {
		return ctrl.Result{}, nil
	}

	location.Status.Status = HealthStatusHealthy
	return ctrl.Result{}, r.Status().Update(ctx, location)
}

func (r *LocationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Location{}).
		Complete(r)
}
