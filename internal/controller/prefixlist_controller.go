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
	sigsbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/consolidation"
	"github.com/EdgeCDN-X/edgecdnx-controller/internal/throwable"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// PrefixListReconciler reconciles a PrefixList object
type PrefixListReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	builder.ThrowerOptions
}

const ConsoliadtionStatusConsolidated = "Consolidated"
const ConsoliadtionStatusConsolidating = "Consolidating"

const SourceController = "Controller"

func (r *PrefixListReconciler) reconcileArgocdApplicationSet(prefixList *infrastructurev1alpha1.PrefixList, ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	resource := &infrastructurev1alpha1.PrefixList{
		TypeMeta: prefixList.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: prefixList.Name,
		},
		Spec: prefixList.Spec,
	}

	prefixesHelmValues := throwable.ThrowerHelmValues{
		Resources: []any{resource},
	}

	appsetBuilder, err := builder.AppsetBuilderFactory("PrefixList", prefixList.Name, prefixList.Namespace, r.ThrowerOptions)
	if err != nil {
		log.Error(err, "Failed to create ApplicationSet builder for PrefixList")
		return ctrl.Result{}, err
	}

	appsetBuilder.SetHelmValues(prefixesHelmValues)

	desiredAppset, hash, err := appsetBuilder.Build()
	if err != nil {
		log.Error(err, "Failed to get ApplicationSet spec for PrefixList")
		return ctrl.Result{}, err
	}

	currAppset := &argoprojv1alpha1.ApplicationSet{}
	err = r.Get(ctx, types.NamespacedName{Namespace: prefixList.Namespace, Name: prefixList.Name}, currAppset)

	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ApplicationSet not found for PrefixList, creating it")
			err := controllerutil.SetControllerReference(prefixList, &desiredAppset, r.Scheme)
			if err != nil {
				log.Error(err, "Failed to set controller reference on ApplicationSet")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, r.Create(ctx, &desiredAppset)
		} else {
			log.Error(err, "Failed to get ApplicationSet for PrefixList")
			return ctrl.Result{}, err
		}
	} else {
		if !currAppset.DeletionTimestamp.IsZero() {
			log.Info("ApplicationSet for PrefixList is being deleted. Skipping reconciliation")
			return ctrl.Result{}, nil
		}

		currAppsetHash, ok := currAppset.ObjectMeta.Annotations[builder.ValuesHashAnnotation]
		if !ok || currAppsetHash != hash {
			log.Info("Updating ApplicationSet for Prefixlist", "name", prefixList.Name)
			currAppset.Spec = desiredAppset.Spec
			currAppset.ObjectMeta.Annotations = desiredAppset.ObjectMeta.Annotations
			return ctrl.Result{}, r.Update(ctx, currAppset)
		}
	}

	if prefixList.Status.Status != HealthStatusHealthy {
		prefixList.Status = infrastructurev1alpha1.PrefixListStatus{
			Status: HealthStatusHealthy,
		}
		return ctrl.Result{}, r.Status().Update(ctx, prefixList)
	}

	return ctrl.Result{}, nil

}

func (r *PrefixListReconciler) handleUserPrefixList(prefixList *infrastructurev1alpha1.PrefixList, ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if prefixList.Status != (infrastructurev1alpha1.PrefixListStatus{}) {
		if prefixList.Status.Status == HealthStatusHealthy {
			generatedName := fmt.Sprintf("%s-%s", prefixList.Spec.Destination, "generated")
			generatedPrefixList := &infrastructurev1alpha1.PrefixList{}

			err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: generatedName}, generatedPrefixList)

			if err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("Generated PrefixList not found, creating it")

					targetPrefixList := &infrastructurev1alpha1.PrefixList{
						ObjectMeta: metav1.ObjectMeta{
							Name:      generatedName,
							Namespace: req.Namespace,
							Annotations: map[string]string{
								builder.ValuesHashAnnotation: "",
							},
						},
						Spec: infrastructurev1alpha1.PrefixListSpec{
							Source: SourceController,
							Prefix: infrastructurev1alpha1.PrefixSpec{
								V4: make([]infrastructurev1alpha1.V4PrefixSpec, 0),
								V6: make([]infrastructurev1alpha1.V6PrefixSpec, 0),
							},
							Destination: prefixList.Spec.Destination,
						},
					}
					err = controllerutil.SetOwnerReference(prefixList, targetPrefixList, r.Scheme)
					if err != nil {
						log.Error(err, "Failed to set owner reference for PrefixList")
						return ctrl.Result{}, err
					}
					return ctrl.Result{}, r.Create(ctx, targetPrefixList)
				} else {
					log.Error(err, "Failed to get Generated PrefixList")
					return ctrl.Result{}, err
				}
			} else {
				if !generatedPrefixList.DeletionTimestamp.IsZero() {
					log.Info("Generated PrefixList is being deleted, skipping reconciliation")
					return ctrl.Result{}, nil
				}

				containsOwnerReference, err := controllerutil.HasOwnerReference(generatedPrefixList.OwnerReferences, prefixList, r.Scheme)
				if err != nil {
					log.Error(err, "Failed to check owner reference for Generated PrefixList")
					return ctrl.Result{}, err
				}

				if !containsOwnerReference {
					log.Info("Prefixlist exists for destination. Adding OwnerReference to generated PrefixList")
					err = controllerutil.SetOwnerReference(prefixList, generatedPrefixList, r.Scheme)
					if err != nil {
						log.Error(err, "Failed to set owner reference for PrefixList")
						return ctrl.Result{}, err
					}
				}

				v4Prefixes := make([]infrastructurev1alpha1.V4PrefixSpec, 0)
				v6Prefixes := make([]infrastructurev1alpha1.V6PrefixSpec, 0)

				for _, ownerRes := range generatedPrefixList.OwnerReferences {
					if ownerRes.Kind == "PrefixList" {
						ownerPrefixList := &infrastructurev1alpha1.PrefixList{}
						err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: ownerRes.Name}, ownerPrefixList)
						if err != nil {
							log.Error(err, "Failed to get Owner PrefixList")
							return ctrl.Result{}, err
						}

						if ownerPrefixList.Status.Status != HealthStatusHealthy {
							log.Info("Owner PrefixList is in progress. Waiting for it to be healthy")
							return ctrl.Result{Requeue: true}, nil
						}

						v4Prefixes = append(v4Prefixes, ownerPrefixList.Spec.Prefix.V4...)
						v6Prefixes = append(v6Prefixes, ownerPrefixList.Spec.Prefix.V6...)
					}
				}

				newPrefixesV4, err := consolidation.ConsolidateV4(ctx, v4Prefixes)
				if err != nil {
					log.Error(err, "Failed to consolidate prefixes")
					return ctrl.Result{}, err
				}

				newPrefixesV6, err := consolidation.ConsolidateV6(ctx, v6Prefixes)
				if err != nil {
					log.Error(err, "Failed to consolidate prefixes")
					return ctrl.Result{}, err
				}

				newPrefix := infrastructurev1alpha1.PrefixSpec{
					V4: newPrefixesV4,
					V6: newPrefixesV6,
				}

				prefixByteA, err := json.Marshal(newPrefix)
				if err != nil {
					log.Error(err, "Failed to marshal prefix object")
					return ctrl.Result{}, err
				}
				newmd5Hash := md5.Sum(prefixByteA)

				curHash, ok := generatedPrefixList.ObjectMeta.Annotations[builder.ValuesHashAnnotation]
				if ok && curHash == fmt.Sprintf("%x", newmd5Hash) {
					log.Info("Generated PrefixList already exists for destination with the correct config (md5-hash).")
					return ctrl.Result{}, nil
				}

				generatedPrefixList.ObjectMeta.Annotations[builder.ValuesHashAnnotation] = fmt.Sprintf("%x", newmd5Hash)
				generatedPrefixList.Spec.Prefix = newPrefix

				return ctrl.Result{}, r.Update(ctx, generatedPrefixList)
			}
		}
	} else {
		log.V(1).Info("PrefixList is not yet initialized, likely just created. Setting status to healthy")
		prefixList.Status = infrastructurev1alpha1.PrefixListStatus{
			Status: HealthStatusHealthy,
		}
		return ctrl.Result{}, r.Status().Update(ctx, prefixList)
	}

	return ctrl.Result{}, nil
}

func (r *PrefixListReconciler) handleControllerPrefixList(prefixList *infrastructurev1alpha1.PrefixList, ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	prefixByteA, err := json.Marshal(prefixList.Spec.Prefix)
	if err != nil {
		log.Error(err, "Failed to marshal prefix object")
		return ctrl.Result{}, err
	}
	newmd5Hash := md5.Sum(prefixByteA)

	curHash, ok := prefixList.ObjectMeta.Annotations[builder.ValuesHashAnnotation]

	if ok && curHash == fmt.Sprintf("%x", newmd5Hash) {

		log.Info("PrefixList already exists for destination with the correct config (md5-hash).")

		if prefixList.Status.Status != HealthStatusHealthy {
			log.Info("Setting Prefixlist status to healthy")
			prefixList.Status = infrastructurev1alpha1.PrefixListStatus{
				Status:              HealthStatusHealthy,
				ConsoliadtionStatus: ConsoliadtionStatusConsolidated,
			}
			return ctrl.Result{}, r.Status().Update(ctx, prefixList)
		}

		return r.reconcileArgocdApplicationSet(prefixList, ctx, req)
	}
	return ctrl.Result{}, nil
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists/finalizers,verbs=update

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *PrefixListReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	prefixList := &infrastructurev1alpha1.PrefixList{}
	err := r.Get(ctx, req.NamespacedName, prefixList)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "Failed to get Prefixlist")
			return ctrl.Result{}, err
		}
	}

	if prefixList.Spec.Source == "Static" || prefixList.Spec.Source == "Bgp" {
		return r.handleUserPrefixList(prefixList, ctx, req)
	}

	if prefixList.Spec.Source == SourceController {
		return r.handleControllerPrefixList(prefixList, ctx, req)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PrefixListReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.PrefixList{}).
		Owns(&argoprojv1alpha1.ApplicationSet{}).
		Owns(&infrastructurev1alpha1.PrefixList{}, sigsbuilder.MatchEveryOwner).
		Complete(r)
}
