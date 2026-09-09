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
	"fmt"
	"strings"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/builder"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// ServiceRoutingReconciler reconciles a Service object
type ServiceRoutingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=dnsendpoints,verbs=get;list;watch;create;update;patch;delete

func (r *ServiceRoutingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	service := &infrastructurev1alpha1.Service{}

	// Object not found
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	desiredNames := make(map[string]struct{})
	dnsNames := []string{service.Spec.Domain}
	for _, hostAlias := range service.Spec.HostAliases {
		dnsNames = append(dnsNames, hostAlias.Name)
	}

	for _, dnsName := range dnsNames {
		for _, recordType := range []string{"A", "AAAA"} {
			name := serviceDNSEndpointName(service.Name, dnsName, recordType)
			desiredNames[name] = struct{}{}

			dnsEndpointBuilder, err := builder.DNSEndpointBuilderFactory("Service", name, service.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}
			dnsEndpointBuilder.WithService(*service, dnsName, recordType)
			desiredDNSEndpoint, hash, err := dnsEndpointBuilder.Build()
			if err != nil {
				return ctrl.Result{}, err
			}

			currentDNSEndpoint := &infrastructurev1alpha1.DNSEndpoint{}
			key := types.NamespacedName{Name: name, Namespace: service.Namespace}
			if err := r.Get(ctx, key, currentDNSEndpoint); err != nil {
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}

				if err := controllerutil.SetControllerReference(service, &desiredDNSEndpoint, r.Scheme); err != nil {
					return ctrl.Result{}, err
				}
				log.Info("Creating DNSEndpoint for Service", "dnsEndpoint", key)
				if err := r.Create(ctx, &desiredDNSEndpoint); err != nil {
					return ctrl.Result{}, err
				}
				continue
			}

			currentHash, hasHash := currentDNSEndpoint.Annotations[builder.ValuesHashAnnotation]
			if hasHash && currentHash == hash && metav1.IsControlledBy(currentDNSEndpoint, service) {
				continue
			}

			currentDNSEndpoint.Spec = desiredDNSEndpoint.Spec
			currentDNSEndpoint.Annotations = desiredDNSEndpoint.Annotations
			if err := controllerutil.SetControllerReference(service, currentDNSEndpoint, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Updating DNSEndpoint for Service", "dnsEndpoint", key, "oldHash", currentHash, "newHash", hash)
			if err := r.Update(ctx, currentDNSEndpoint); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	dnsEndpoints := &infrastructurev1alpha1.DNSEndpointList{}
	if err := r.List(ctx, dnsEndpoints, client.InNamespace(service.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	for i := range dnsEndpoints.Items {
		dnsEndpoint := &dnsEndpoints.Items[i]
		if !metav1.IsControlledBy(dnsEndpoint, service) {
			continue
		}
		if _, desired := desiredNames[dnsEndpoint.Name]; desired {
			continue
		}

		log.Info("Deleting stale DNSEndpoint for Service", "dnsEndpoint", client.ObjectKeyFromObject(dnsEndpoint))
		if err := r.Delete(ctx, dnsEndpoint); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func serviceDNSEndpointName(serviceName string, dnsName string, recordType string) string {
	dnsNameHash := md5.Sum([]byte(strings.ToLower(strings.TrimSuffix(dnsName, "."))))
	suffix := fmt.Sprintf("-%x-%s", dnsNameHash[:6], strings.ToLower(recordType))
	maxServiceNameLength := validation.DNS1123SubdomainMaxLength - len(suffix)
	if len(serviceName) > maxServiceNameLength {
		serviceName = strings.TrimRight(serviceName[:maxServiceNameLength], "-.")
	}

	return serviceName + suffix
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceRoutingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.Service{}).
		Owns(&infrastructurev1alpha1.DNSEndpoint{}).
		Complete(r)
}
