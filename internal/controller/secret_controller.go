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
	"fmt"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// LocationReconciler reconciles a Location object
type SecretReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ThrowerOptions builder.ThrowerOptions
}

const (
	ResourceTypeLabel        = "edgecdnx.com/resource-type"
	MonitoringTLSSecretValue = "monitoring-tls"
	AnalyticsTLSSecretValue  = "analytics-tls"
)

func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	secret := &v1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		log.Error(err, "unable to fetch Secret")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	labelValue, ok := secret.Labels[ResourceTypeLabel]

	if !ok {
		return ctrl.Result{}, nil
	}

	if labelValue == MonitoringTLSSecretValue || labelValue == AnalyticsTLSSecretValue {
		log.Info(fmt.Sprintf("Reconciling %s TLS Secret", labelValue), "name", secret.Name, "namespace", secret.Namespace)

		targetValue, ok := secret.Annotations["target"]
		if !ok {
			log.Info("No target annotation found on Secret, skipping")
			return ctrl.Result{}, nil
		}

		targetName, ok := secret.Annotations["targetName"]
		if !ok {
			log.Info("No targetName annotation found on Secret, skipping")
			return ctrl.Result{}, nil
		}

		if targetName == "in-cluster" {
			log.V(1).Info("TargetName is in-cluster, not generating Application")
			return ctrl.Result{}, nil
		}

		throwerAppBuilder, err := builder.AppBuilderFactory(labelValue, secret.Name, secret.Namespace, targetValue, r.ThrowerOptions)
		if err != nil {
			log.Error(err, "unable to create thrower builder")
			return ctrl.Result{}, err
		}

		secretRawValue := &v1.Secret{
			TypeMeta: secret.TypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name: secret.Name,
			},
			Data: secret.Data,
			Type: v1.SecretTypeTLS,
		}

		secretHelmValues := struct {
			Resources []any `json:"resources"`
		}{
			Resources: []any{secretRawValue},
		}
		throwerAppBuilder.WithHelmValues(secretHelmValues)
		desiredApp, hash, err := throwerAppBuilder.Build()
		if err != nil {
			log.Error(err, "unable to build application")
			return ctrl.Result{}, err
		}

		currApp := &argoprojv1alpha1.Application{}
		err = r.Get(ctx, types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}, currApp)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Creating Application for TLS Secret", "name", secret.Name)

				err = controllerutil.SetControllerReference(secret, &desiredApp, r.Scheme)
				if err != nil {
					log.Error(err, "unable to set owner reference on Application for TLS Secret", "Secret", secret.Name)
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, r.Create(ctx, &desiredApp)
			}

			log.Error(err, "unable to fetch Application for TLS Secret")
			return ctrl.Result{}, err
		} else {
			if !currApp.DeletionTimestamp.IsZero() {
				log.Info("Application is being deleted, skipping update", "name", secret.Name)
				return ctrl.Result{}, nil
			}

			currAppHash, ok := currApp.Annotations[builder.ValuesHashAnnotation]
			if !ok || currAppHash != hash {
				log.Info("Updating Application for TLS Secret", "name", secret.Name)
				currApp.Spec = desiredApp.Spec
				currApp.Annotations = desiredApp.Annotations
				return ctrl.Result{}, r.Update(ctx, currApp)
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Secret{}).
		Owns(&argoprojv1alpha1.Application{}).
		Complete(r)
}
