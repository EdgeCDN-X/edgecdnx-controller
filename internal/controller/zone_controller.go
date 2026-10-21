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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

const zoneCleanupFinalizer = "edgecdnx.com/zone-cleanup"

// ZoneReconciler reconciles a Zone object
type ZoneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=zones,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=zones/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=zones/finalizers,verbs=update
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=dnsendpoints,verbs=get;list;watch;delete
// Reconcile is part of the main kubernetes reconciliation loop which aims to

func (r *ZoneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	zone := &infrastructurev1alpha1.Zone{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, zone); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Zone")
			return ctrl.Result{}, err
		}
		log.Info("Zone resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	if !zone.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(zone, zoneCleanupFinalizer) {
			return ctrl.Result{}, nil
		}

		endpoints := &infrastructurev1alpha1.DNSEndpointList{}
		if err := r.List(ctx, endpoints, client.InNamespace(zone.Namespace)); err != nil {
			return ctrl.Result{}, err
		}

		pending := false
		for index := range endpoints.Items {
			endpoint := &endpoints.Items[index]
			if endpoint.Spec.Zone == nil || endpoint.Spec.Zone.Name != zone.Name {
				continue
			}
			pending = true
			if endpoint.DeletionTimestamp.IsZero() {
				if err := client.IgnoreNotFound(r.Delete(ctx, endpoint)); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
		if pending {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		controllerutil.RemoveFinalizer(zone, zoneCleanupFinalizer)
		return ctrl.Result{}, r.Update(ctx, zone)
	}

	if controllerutil.AddFinalizer(zone, zoneCleanupFinalizer) {
		if err := r.Update(ctx, zone); err != nil {
			return ctrl.Result{}, err
		}
	}

	if zone.Status.Status != HealthStatusHealthy {
		zone.Status = infrastructurev1alpha1.ZoneStatus{
			Status: HealthStatusHealthy,
		}
		return ctrl.Result{}, r.Status().Update(ctx, zone)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ZoneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Zone{}).
		Complete(r)
}
