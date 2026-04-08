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

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	monv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

// LocationRoutingReconciler reconciles a Location object
type LocationRoutingReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	ManageMonitoringProbes bool
	ProbeBuilderConfig     builder.ProbeBuilderConfig
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=locations/finalizers,verbs=update
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=probes,verbs=get;list;watch;create;update;patch;delete

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

	if location.DeletionTimestamp != nil {
		// No finalizer logic for now, just ignore deletion events
		return ctrl.Result{}, nil
	}

	if r.ManageMonitoringProbes {
		if err := r.reconcileLocationProbe(ctx, location); err != nil {
			return ctrl.Result{}, err
		}
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
		Owns(&monv1.Probe{}).
		Complete(r)
}

func (r *LocationRoutingReconciler) reconcileLocationProbe(ctx context.Context, location *infrastructurev1alpha1.Location) error {
	log := logf.FromContext(ctx)

	probeKey := types.NamespacedName{Name: location.Name, Namespace: location.Namespace}
	probeBuilder, err := builder.ProbeBuilderFactory("Location", location.Name, location.Namespace, r.ProbeBuilderConfig)
	if err != nil {
		return err
	}

	probeBuilder.WithLocation(*location)
	desiredProbe, _, err := probeBuilder.Build()
	if err != nil {
		return err
	}

	if desiredProbe.Spec.Targets.StaticConfig == nil || len(desiredProbe.Spec.Targets.StaticConfig.Targets) == 0 {
		probe := &monv1.Probe{}
		if err := r.Get(ctx, probeKey, probe); err != nil {
			return client.IgnoreNotFound(err)
		}

		return r.Delete(ctx, probe)
	}

	probe := &monv1.Probe{ObjectMeta: metav1.ObjectMeta{Name: location.Name, Namespace: location.Namespace}}

	res, err := controllerutil.CreateOrUpdate(ctx, r.Client, probe, func() error {
		probe.Labels = desiredProbe.Labels
		probe.Annotations = desiredProbe.Annotations
		probe.Spec = desiredProbe.Spec
		return controllerutil.SetControllerReference(location, probe, r.Scheme)
	})

	log.Info("Reconciled Probe for Location", "probe", probeKey, "result", res, "err", err)

	return err
}
