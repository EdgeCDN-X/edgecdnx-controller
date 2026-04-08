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
	"slices"

	"github.com/miekg/dns"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var zonelog = logf.Log.WithName("zone-resource")

// SetupZoneWebhookWithManager registers the webhook for Zone in the manager.
func SetupZoneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1alpha1.Zone{}).
		WithValidator(&ZoneCustomValidator{
			Client: mgr.GetClient(),
		}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-infrastructure-edgecdnx-com-v1alpha1-zone,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=zones,verbs=create;update,versions=v1alpha1,name=vzone-v1alpha1.kb.io,admissionReviewVersions=v1

// ZoneCustomValidator struct is responsible for validating the Zone resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ZoneCustomValidator struct {
	Client client.Client
}

func (v *ZoneCustomValidator) ValidateZoneOverlaps(ctx context.Context, zone *infrastructurev1alpha1.Zone) (admission.Warnings, error) {

	zones := &infrastructurev1alpha1.ZoneList{}
	if err := v.Client.List(ctx, zones); err != nil {
		return nil, fmt.Errorf("failed to list existing zones: %w", err)
	}

	var overlappingZones []string
	for _, existingZone := range zones.Items {
		if existingZone.Name == zone.Name {
			continue // skip self on update
		}
		if dns.IsSubDomain(fmt.Sprintf("%s.", zone.Spec.Zone), fmt.Sprintf("%s.", existingZone.Spec.Zone)) || dns.IsSubDomain(fmt.Sprintf("%s.", existingZone.Spec.Zone), fmt.Sprintf("%s.", zone.Spec.Zone)) {
			overlappingZones = append(overlappingZones, existingZone.Name)
		}
	}

	if len(overlappingZones) > 0 {
		warnings := make(admission.Warnings, 0, len(overlappingZones))
		for _, overlappingZone := range overlappingZones {
			warnings = append(warnings, fmt.Sprintf("zone %q overlaps with existing zone %q", zone.Spec.Zone, overlappingZone))
		}
		return warnings, fmt.Errorf("zone %q overlaps with existing zones: %v", zone.Spec.Zone, overlappingZones)
	}

	return nil, nil
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Zone.
func (v *ZoneCustomValidator) ValidateCreate(ctx context.Context, obj *infrastructurev1alpha1.Zone) (admission.Warnings, error) {
	zonelog.Info("Validation for Zone upon creation", "name", obj.GetName())
	return v.ValidateZoneOverlaps(ctx, obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Zone.
func (v *ZoneCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *infrastructurev1alpha1.Zone) (admission.Warnings, error) {
	zonelog.Info("Validation for Zone upon update", "name", newObj.GetName())

	return v.ValidateZoneOverlaps(ctx, newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Zone.
func (v *ZoneCustomValidator) ValidateDelete(ctx context.Context, obj *infrastructurev1alpha1.Zone) (admission.Warnings, error) {
	zonelog.Info("Validation for Zone upon deletion", "name", obj.GetName())

	services := &infrastructurev1alpha1.ServiceList{}
	if err := v.Client.List(ctx, services); err != nil {
		return nil, fmt.Errorf("failed to list existing services: %w", err)
	}

	var servicesUsingZone []string
	for _, service := range services.Items {
		if dns.IsSubDomain(fmt.Sprintf("%s.", obj.Spec.Zone), fmt.Sprintf("%s.", service.Spec.Domain)) {
			servicesUsingZone = append(servicesUsingZone, service.Name)
		}

		if slices.ContainsFunc(service.Spec.HostAliases, func(alias infrastructurev1alpha1.HostAliasSpec) bool {
			return dns.IsSubDomain(fmt.Sprintf("%s.", obj.Spec.Zone), fmt.Sprintf("%s.", alias.Name))
		}) {
			servicesUsingZone = append(servicesUsingZone, service.Name)
		}
	}

	if len(servicesUsingZone) > 0 {
		warnings := make(admission.Warnings, 0, len(servicesUsingZone))
		for _, service := range servicesUsingZone {
			warnings = append(warnings, fmt.Sprintf("zone %q is still in use by service %q", obj.Spec.Zone, service))
		}
		return warnings, fmt.Errorf("zone %q is still in use by services: %v", obj.Spec.Zone, servicesUsingZone)
	}

	return nil, nil
}
