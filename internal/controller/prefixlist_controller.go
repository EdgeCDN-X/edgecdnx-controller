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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller.git/api/v1alpha1"
	"github.com/EdgeCDN-X/edgecdnx-controller.git/internal/consolidation"
	"github.com/EdgeCDN-X/edgecdnx-controller.git/internal/throwable"
	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// PrefixListReconciler reconciles a PrefixList object
type PrefixListReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	ThrowerOptions
}

const ConsoliadtionStatusConsolidated = "Consolidated"
const HealthStatusProgressing = "Progressing"

const SourceController = "Controller"

func (r *PrefixListReconciler) reconcileArgocdApplicationSet(prefixList *infrastructurev1alpha1.PrefixList, ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	appset := &argoprojv1alpha1.ApplicationSet{}

	resource := &infrastructurev1alpha1.PrefixList{
		TypeMeta: prefixList.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      prefixList.Name,
			Namespace: r.InfrastructureTargetNamespace,
		},
		Spec: prefixList.Spec,
	}

	prefixesHelmValues := throwable.ThrowerHelmValues{
		Resources: []any{resource},
	}

	desiredAppSpec, err := prefixesHelmValues.GetAppSetSpec(r.ThrowerChartRepository,
		r.ThrowerChartName,
		r.ThrowerChartVersion,
		r.InfrastructureApplicationSetNamespace,
		r.InfrastructureApplicationSetProject,
		r.InfrastructureTargetNamespace,
		fmt.Sprintf(`{{ metadata.labels.edgecdnx.com/location }}-prefixes-%s`,
			prefixList.Name))

	md5Hash, err := prefixesHelmValues.GetMd5Hash()
	if err != nil {
		log.Error(err, "Failed to get md5 hash for Helm values")
		return ctrl.Result{}, err
	}

	err = r.Get(ctx, types.NamespacedName{Namespace: r.InfrastructureApplicationSetNamespace, Name: prefixList.Name}, appset)

	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ApplicationSet not found for PrefixList, creating it")
			appset = &argoprojv1alpha1.ApplicationSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      prefixList.Name,
					Namespace: r.InfrastructureApplicationSetNamespace,
					Annotations: map[string]string{
						ValuesHashAnnotation: md5Hash,
					},
				},
				Spec: desiredAppSpec,
			}

			controllerutil.SetControllerReference(prefixList, appset, r.Scheme)
			return ctrl.Result{}, r.Create(ctx, appset)
		} else {
			log.Error(err, "Failed to get ApplicationSet for PrefixList")
			return ctrl.Result{}, err
		}
	}

	if !appset.DeletionTimestamp.IsZero() {
		log.Info("ApplicationSet for PrefixList is being deleted. Skipping reconciliation")
		return ctrl.Result{}, nil
	}

	currAppsetHash, ok := appset.ObjectMeta.Annotations[ValuesHashAnnotation]
	if !ok || currAppsetHash != md5Hash {
		log.Info("Updating ApplicationSet for Location")

		appset.Spec = desiredAppSpec
		appset.ObjectMeta.Annotations[ValuesHashAnnotation] = md5Hash

		return ctrl.Result{}, r.Update(ctx, appset)
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

			if err != nil && apierrors.IsNotFound(err) {
				log.Info("Generated PrefixList not found, creating it")

				targetPrefixList := &infrastructurev1alpha1.PrefixList{
					ObjectMeta: metav1.ObjectMeta{
						Name:      generatedName,
						Namespace: req.Namespace,
						Annotations: map[string]string{
							"edgecdnx.com/config-md5": "",
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

				controllerutil.SetOwnerReference(prefixList, targetPrefixList, r.Scheme)
				return ctrl.Result{}, r.Create(ctx, targetPrefixList)
			} else {

				if !generatedPrefixList.DeletionTimestamp.IsZero() {
					log.Info("Generated PrefixList is being deleted, skipping reconciliation")
					return ctrl.Result{}, nil
				}

				containsOwnerReference := false
				for _, ownerRef := range generatedPrefixList.OwnerReferences {
					if ownerRef.UID == prefixList.UID {
						containsOwnerReference = true
						break
					}
				}

				if !containsOwnerReference {
					log.Info("Prefixlist exists for destination. Adding OwnerReference to generated PrefixList")
					controllerutil.SetOwnerReference(prefixList, generatedPrefixList, r.Scheme)
					return ctrl.Result{}, r.Update(ctx, generatedPrefixList)
				}

				return ctrl.Result{}, nil
			}
		}

	} else {
		log.Info("PrefixList is not yet initialized, setting status to healthy")
		prefixList.Status = infrastructurev1alpha1.PrefixListStatus{
			Status: HealthStatusHealthy,
		}
		return ctrl.Result{}, r.Status().Update(ctx, prefixList)
	}

	return ctrl.Result{}, nil
}

func (r *PrefixListReconciler) handleControllerPrefixList(prefixList *infrastructurev1alpha1.PrefixList, ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	v4Prefixes := make([]infrastructurev1alpha1.V4PrefixSpec, 0)
	v6Prefixes := make([]infrastructurev1alpha1.V6PrefixSpec, 0)

	for _, ownerRes := range prefixList.OwnerReferences {
		if ownerRes.Kind == "PrefixList" {
			log.Info(fmt.Sprintf("%v", ownerRes))
			ownerPrefixList := &infrastructurev1alpha1.PrefixList{}
			err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: ownerRes.Name}, ownerPrefixList)
			if err != nil {
				log.Error(err, "Failed to get Owner PrefixList")
				return ctrl.Result{}, err
			}

			if ownerPrefixList.Status.Status != HealthStatusHealthy {
				log.Info("Owner PrefixList is in progress. Waiting for it to be healthy")
				return ctrl.Result{}, nil
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
	newPrefixesV6 := v6Prefixes

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

	curHash, ok := prefixList.ObjectMeta.Annotations[ValuesHashAnnotation]

	if ok && curHash == fmt.Sprintf("%x", newmd5Hash) {

		log.Info("PrefixList already exists for destination with the correct config (md5-hash).")

		prefixList.Status = infrastructurev1alpha1.PrefixListStatus{
			Status:              HealthStatusHealthy,
			ConsoliadtionStatus: ConsoliadtionStatusConsolidated,
		}
		r.Status().Update(ctx, prefixList)

		return r.reconcileArgocdApplicationSet(prefixList, ctx, req)
	}

	prefixList.ObjectMeta.Annotations[ValuesHashAnnotation] = fmt.Sprintf("%x", newmd5Hash)
	prefixList.Spec.Prefix = newPrefix

	return ctrl.Result{}, r.Update(ctx, prefixList)
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=prefixlists/finalizers,verbs=update
// +kubebuilder:rbac:groups=argoproj.io,resources=applicationsets,verbs=get;list;watch;create;update;patch;delete

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
		Owns(&infrastructurev1alpha1.PrefixList{}, builder.MatchEveryOwner).
		Complete(r)
}
