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

package v1alpha1

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var dnsendpointlog = logf.Log.WithName("dnsendpoint-resource")

// SetupDNSEndpointWebhookWithManager registers the webhook for DNSEndpoint in the manager.
func SetupDNSEndpointWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1alpha1.DNSEndpoint{}).
		WithValidator(&DNSEndpointCustomValidator{
			Client: mgr.GetClient(),
		}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-infrastructure-edgecdnx-com-v1alpha1-dnsendpoint,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=dnsendpoints,verbs=create;update,versions=v1alpha1,name=vdnsendpoint-v1alpha1.kb.io,admissionReviewVersions=v1

// DNSEndpointCustomValidator struct is responsible for validating the DNSEndpoint resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type DNSEndpointCustomValidator struct {
	Client client.Client
}

func (v *DNSEndpointCustomValidator) validateSpec(ctx context.Context, dnsendpoint *infrastructurev1alpha1.DNSEndpoint) (admission.Warnings, error) {
	switch dnsendpoint.Spec.RoutingPolicy {
	case "Weighted", "Geolocation", "RoundRobin":
		if dnsendpoint.Spec.RouteSelector == nil {
			return nil, fmt.Errorf("spec.routeSelector is required when routingPolicy is %q", dnsendpoint.Spec.RoutingPolicy)
		}
	}

	if dnsendpoint.Spec.Zone == nil || dnsendpoint.Spec.Zone.Name == "" {
		return nil, fmt.Errorf("spec.zone is required")
	}

	zone := &infrastructurev1alpha1.Zone{}
	zoneKey := types.NamespacedName{Namespace: dnsendpoint.Namespace, Name: dnsendpoint.Spec.Zone.Name}
	if err := v.Client.Get(ctx, zoneKey, zone); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("zone %q does not exist in namespace %q", zoneKey.Name, zoneKey.Namespace)
		}
		return nil, fmt.Errorf("failed to get zone %q: %w", zoneKey.Name, err)
	}

	return nil, nil
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type DNSEndpoint.
func (v *DNSEndpointCustomValidator) ValidateCreate(ctx context.Context, obj *infrastructurev1alpha1.DNSEndpoint) (admission.Warnings, error) {
	dnsendpoint := obj
	dnsendpointlog.Info("Validation for DNSEndpoint upon creation", "name", dnsendpoint.GetName())

	return v.validateSpec(ctx, dnsendpoint)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type DNSEndpoint.
func (v *DNSEndpointCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *infrastructurev1alpha1.DNSEndpoint) (admission.Warnings, error) {
	dnsendpoint := newObj
	dnsendpointlog.Info("Validation for DNSEndpoint upon update", "name", dnsendpoint.GetName())

	return v.validateSpec(ctx, dnsendpoint)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type DNSEndpoint.
func (v *DNSEndpointCustomValidator) ValidateDelete(ctx context.Context, obj *infrastructurev1alpha1.DNSEndpoint) (admission.Warnings, error) {
	dnsendpoint := obj
	dnsendpointlog.Info("Validation for DNSEndpoint upon deletion", "name", dnsendpoint.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
