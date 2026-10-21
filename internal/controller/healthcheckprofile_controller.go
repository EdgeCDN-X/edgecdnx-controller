package controller

import (
	"context"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type HealthCheckProfileReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=healthcheckprofiles,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=healthcheckprofiles/status,verbs=get;update;patch

func (r *HealthCheckProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	profile := &infrastructurev1alpha1.HealthCheckProfile{}
	if err := r.Get(ctx, req.NamespacedName, profile); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !profile.DeletionTimestamp.IsZero() || profile.Status.Status == HealthStatusHealthy {
		return ctrl.Result{}, nil
	}

	profile.Status.Status = HealthStatusHealthy
	return ctrl.Result{}, r.Status().Update(ctx, profile)
}

func (r *HealthCheckProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.HealthCheckProfile{}).
		Complete(r)
}
