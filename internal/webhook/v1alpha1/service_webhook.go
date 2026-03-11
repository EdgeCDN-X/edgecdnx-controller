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
	"net"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// nolint:unused
// log is for logging in this package.
var servicelog = logf.Log.WithName("service-resource")

// SetupServiceWebhookWithManager registers the webhook for Service in the manager.
func SetupServiceWebhookWithManager(mgr ctrl.Manager, blockedUpstreamTLDs []string) error {

	return ctrl.NewWebhookManagedBy(mgr).For(&infrastructurev1alpha1.Service{}).
		WithValidator(&ServiceCustomValidator{
			Client:              mgr.GetClient(),
			BlockedUpstreamTLDs: normaliseUpstreamTLDs(blockedUpstreamTLDs),
		}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-infrastructure-edgecdnx-com-v1alpha1-service,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=services,verbs=create;update,versions=v1alpha1,name=vservice-v1alpha1.kb.io,admissionReviewVersions=v1

// ServiceCustomValidator struct is responsible for validating the Service resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ServiceCustomValidator struct {
	Client              client.Client
	BlockedUpstreamTLDs []string
}

var _ webhook.CustomValidator = &ServiceCustomValidator{}

func normaliseUpstreamTLDs(tlds []string) []string {
	normalizedTLDs := make([]string, 0, len(tlds))
	for _, tld := range tlds {
		normalizedTLD := strings.TrimSpace(strings.ToLower(tld))
		normalizedTLD = strings.TrimPrefix(normalizedTLD, ".")
		if normalizedTLD == "" {
			continue
		}
		normalizedTLDs = append(normalizedTLDs, normalizedTLD)
	}

	if len(normalizedTLDs) == 0 {
		return []string{}
	}

	return normalizedTLDs
}

func (v *ServiceCustomValidator) ValidateService(ctx context.Context, service *infrastructurev1alpha1.Service) (admission.Warnings, error) {
	services := &infrastructurev1alpha1.ServiceList{}
	err := v.Client.List(ctx, services)
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	warnings := make([]string, 0)

	// Check for hostAlias or domain name duplicates
	for _, svc := range services.Items {
		if svc.Spec.Domain == service.Spec.Domain && svc.Name != service.Name && svc.Namespace != service.Namespace {
			// Do not validate against self
			warnings = append(warnings, fmt.Sprintf("domain %s is already used by Service %s/%s", service.Spec.Domain, svc.Namespace, svc.Name))
			return warnings, fmt.Errorf("domain %s is already used by Service %s/%s", service.Spec.Domain, svc.Namespace, svc.Name)
		}

		for _, hostAlias := range svc.Spec.HostAliases {
			// Validate against self too
			if slices.ContainsFunc(service.Spec.HostAliases, func(ha infrastructurev1alpha1.HostAliasSpec) bool {
				if svc.Name == service.Name && svc.Namespace == service.Namespace {
					// Do not validate against self
					return false
				}
				return ha.Name == hostAlias.Name
			}) {
				warnings = append(warnings, fmt.Sprintf("hostAlias %s is already used by Service %s/%s", hostAlias.Name, svc.Namespace, svc.Name))
				return warnings, fmt.Errorf("hostAlias %s is already used by Service %s/%s", hostAlias.Name, svc.Namespace, svc.Name)
			}
		}
	}

	// Check if upstream configuration is valid
	if service.Spec.OriginType == "static" {
		if len(service.Spec.StaticOrigins) == 0 {
			return nil, fmt.Errorf("originType is set to static but no staticOrigins are defined")
		}

		for _, staticOrigin := range service.Spec.StaticOrigins {
			if staticOrigin.Upstream == "" {
				return nil, fmt.Errorf("static origin has an empty upstream")
			}

			if err := v.validateUpstreamConfiguration(staticOrigin.Upstream); err != nil {
				return nil, err
			}

			if staticOrigin.HostHeader != "" {
				if err := v.validateUpstreamConfiguration(staticOrigin.HostHeader); err != nil {
					return nil, fmt.Errorf("invalid hostHeader %q: %w", staticOrigin.HostHeader, err)
				}
			}
		}
	}

	return nil, nil
}

func (v *ServiceCustomValidator) validateUpstreamConfiguration(upstream string) error {
	trimmedUpstream := strings.TrimSpace(upstream)
	if trimmedUpstream == "" {
		return fmt.Errorf("static origin has an empty upstream")
	}

	ip := net.ParseIP(trimmedUpstream)
	if ip != nil {
		ipv4 := ip.To4()
		if ipv4 == nil {
			return fmt.Errorf("upstream %q must be a public IPv4 address or an FQDN", upstream)
		}

		if ip.IsPrivate() {
			return fmt.Errorf("upstream %q is a private IPv4 address, which is not allowed", upstream)
		}

		return nil
	}

	if !isFQDN(trimmedUpstream) {
		return fmt.Errorf("upstream %q must be a public IPv4 address or an FQDN", upstream)
	}

	normalizedHost := strings.TrimSuffix(strings.ToLower(trimmedUpstream), ".")
	for _, blockedTLD := range v.BlockedUpstreamTLDs {
		if strings.HasSuffix(normalizedHost, "."+blockedTLD) {
			return fmt.Errorf("upstream %q uses blocked top-level domain .%s", upstream, blockedTLD)
		}
	}

	return nil
}

func isFQDN(host string) bool {
	normalizedHost := strings.TrimSuffix(host, ".")
	if len(normalizedHost) == 0 || len(normalizedHost) > 253 {
		return false
	}

	labels := strings.Split(normalizedHost, ".")
	if len(labels) < 2 {
		return false
	}

	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}

		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}

		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' {
				continue
			}

			return false
		}
	}

	return true
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *ServiceCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	service, ok := obj.(*infrastructurev1alpha1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object but got %T", obj)
	}
	servicelog.Info("Validation for Service upon creation", "name", service.GetName())
	return v.ValidateService(ctx, service)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *ServiceCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	service, ok := newObj.(*infrastructurev1alpha1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object for the newObj but got %T", newObj)
	}
	servicelog.Info("Validation for Service upon update", "name", service.GetName())

	// TODO(user): fill in your validation logic upon object update.
	return v.ValidateService(ctx, service)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *ServiceCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	service, ok := obj.(*infrastructurev1alpha1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object but got %T", obj)
	}
	servicelog.Info("Validation for Service upon deletion", "name", service.GetName())

	return nil, nil
}
