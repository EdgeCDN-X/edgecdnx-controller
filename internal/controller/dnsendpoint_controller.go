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

// DNSEndpointReconciler reconciles a DNSEndpoint object
type DNSEndpointReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ThrowerOptions builder.ThrowerOptions
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=dnsendpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=dnsendpoints/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=dnsendpoints/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *DNSEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	dnsEndpoint := &infrastructurev1alpha1.DNSEndpoint{}
	if err := r.Get(ctx, req.NamespacedName, dnsEndpoint); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if dnsEndpoint.Status.Status == HealthStatusProgressing || dnsEndpoint.Status.Status == HealthStatusHealthy {
		// Strip off resource from Namespace. Applicationset Will take care of it.
		resource := &infrastructurev1alpha1.DNSEndpoint{
			TypeMeta: dnsEndpoint.TypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:   dnsEndpoint.Name,
				Labels: dnsEndpoint.Labels,
			},
			Spec: dnsEndpoint.Spec,
		}

		dnsEndpointHelmValues := struct {
			Resources []any `json:"resources"`
		}{
			Resources: []any{resource},
		}

		appsetBuilder, err := builder.AppsetBuilderFactory("DNSEndpoint", dnsEndpoint.Name, dnsEndpoint.Namespace, r.ThrowerOptions)
		if err != nil {
			log.Error(err, "Failed to create ApplicationSet builder for DNSEndpoint")
			return ctrl.Result{}, err
		}
		appsetBuilder.WithHelmValues(dnsEndpointHelmValues)
		desiredAppset, hash, err := appsetBuilder.Build()

		if err != nil {
			log.Error(err, "Failed to get ApplicationSet for DNSEndpoint")
			return ctrl.Result{}, err
		}

		currAppset := &argoprojv1alpha1.ApplicationSet{}
		err = r.Get(ctx, types.NamespacedName{Namespace: dnsEndpoint.Namespace, Name: dnsEndpoint.Name}, currAppset)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating ApplicationSet for DNSEndpoint", "hash", hash)

				err := controllerutil.SetControllerReference(dnsEndpoint, &desiredAppset, r.Scheme)
				if err != nil {
					log.Error(err, "Failed to set controller reference on ApplicationSet")
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, r.Create(ctx, &desiredAppset)
			} else {
				log.Error(err, "Failed to get ApplicationSet for DNSEndpoint")
				return ctrl.Result{}, err
			}
		} else {
			if !currAppset.DeletionTimestamp.IsZero() {
				log.Info("ApplicationSet for DNSEndpoint is being deleted. Skipping reconciliation")
				return ctrl.Result{}, nil
			}

			currAppsetHash, ok := currAppset.Annotations[builder.ValuesHashAnnotation]
			if !ok || currAppsetHash != hash {
				log.Info("Updating ApplicationSet for DNSEndpoint", "old-hash", currAppsetHash, "new-hash", hash)
				currAppset.Spec = desiredAppset.Spec
				currAppset.Annotations = desiredAppset.Annotations
				return ctrl.Result{}, r.Update(ctx, currAppset)
			}
		}

		if dnsEndpoint.Status.Status != HealthStatusHealthy {
			dnsEndpoint.Status = infrastructurev1alpha1.DNSEndpointStatus{
				Status: HealthStatusHealthy,
			}
			return ctrl.Result{}, r.Status().Update(ctx, dnsEndpoint)
		}

		return ctrl.Result{}, nil

	} else {
		// Update status to progressing
		dnsEndpoint.Status = infrastructurev1alpha1.DNSEndpointStatus{
			Status: HealthStatusProgressing,
		}
		return ctrl.Result{}, r.Status().Update(ctx, dnsEndpoint)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.DNSEndpoint{}).
		Named("dnsendpoint").
		Complete(r)
}
