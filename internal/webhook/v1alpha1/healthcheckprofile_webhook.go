/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package v1alpha1

import (
	"context"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// SetupHealthCheckProfileWebhookWithManager registers the webhook for HealthCheckProfile.
func SetupHealthCheckProfileWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1alpha1.HealthCheckProfile{}).
		WithValidator(&HealthCheckProfileCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-edgecdnx-com-v1alpha1-healthcheckprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=healthcheckprofiles,verbs=create;update,versions=v1alpha1,name=vhealthcheckprofile-v1alpha1.kb.io,admissionReviewVersions=v1

// HealthCheckProfileCustomValidator validates health-check probe durations.
type HealthCheckProfileCustomValidator struct{}

func (v *HealthCheckProfileCustomValidator) ValidateCreate(_ context.Context, profile *infrastructurev1alpha1.HealthCheckProfile) (admission.Warnings, error) {
	return validateHealthCheckProfileProbeDurations(profile)
}

func (v *HealthCheckProfileCustomValidator) ValidateUpdate(_ context.Context, _, profile *infrastructurev1alpha1.HealthCheckProfile) (admission.Warnings, error) {
	return validateHealthCheckProfileProbeDurations(profile)
}

func (v *HealthCheckProfileCustomValidator) ValidateDelete(_ context.Context, _ *infrastructurev1alpha1.HealthCheckProfile) (admission.Warnings, error) {
	return nil, nil
}

func validateHealthCheckProfileProbeDurations(profile *infrastructurev1alpha1.HealthCheckProfile) (admission.Warnings, error) {
	for _, probe := range profile.Spec.Probes {
		if probe.Interval.Duration < 5*time.Second {
			return nil, fmt.Errorf("probe %q interval must be at least 5s", probe.Name)
		}
		if probe.Timeout.Duration <= 0 || probe.Timeout.Duration >= probe.Interval.Duration {
			return nil, fmt.Errorf("probe %q timeout must be greater than 0 and less than its interval", probe.Name)
		}
	}
	return nil, nil
}
