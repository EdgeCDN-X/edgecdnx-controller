/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package v1alpha1

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	"github.com/miekg/dns"
)

var locationlog = logf.Log.WithName("location-resource")

// SetupLocationWebhookWithManager registers the webhook for Location.
func SetupLocationWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1alpha1.Location{}).
		WithValidator(&LocationCustomValidator{
			Client: mgr.GetClient(),
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-edgecdnx-com-v1alpha1-location,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=locations,verbs=create;update,versions=v1alpha1,name=vlocation-v1alpha1.kb.io,admissionReviewVersions=v1

// LocationCustomValidator validates health-check probe durations.
type LocationCustomValidator struct {
	Client client.Client
}

func (v *LocationCustomValidator) ValidateCreate(ctx context.Context, profile *infrastructurev1alpha1.Location) (admission.Warnings, error) {
	locationlog.Info("Validating create for location", "location", profile.Name)
	return v.validateLocationSpec(ctx, profile)
}

func (v *LocationCustomValidator) ValidateUpdate(ctx context.Context, _, profile *infrastructurev1alpha1.Location) (admission.Warnings, error) {
	locationlog.Info("Validating update for location", "location", profile.Name)
	return v.validateLocationSpec(ctx, profile)
}

func (v *LocationCustomValidator) ValidateDelete(ctx context.Context, profile *infrastructurev1alpha1.Location) (admission.Warnings, error) {
	locationlog.Info("Validating delete for location", "location", profile.Name)
	return nil, nil
}

func (v *LocationCustomValidator) validateLocationSpec(ctx context.Context, location *infrastructurev1alpha1.Location) (admission.Warnings, error) {

	if location.Spec.BaseDomain != "" {
		zone := &infrastructurev1alpha1.ZoneList{}

		err := v.Client.List(ctx, zone, &client.ListOptions{
			Namespace: location.Namespace,
		})
		if err != nil {
			// If there is an error listing zones, return it as a validation error.
			return nil, err
		}

		zoneBaseDomain := dns.Fqdn(location.Spec.BaseDomain)
		found := false
		for _, z := range zone.Items {
			if dns.IsSubDomain(dns.Fqdn(z.Spec.Zone), zoneBaseDomain) && z.Spec.Project.Name == location.Spec.Project.Name {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("base domain %s is not a subdomain of any zone in project %s", location.Spec.BaseDomain, location.Spec.Project.Name)
		}
	}

	uniqueHosts := make(map[string]struct {
		groupName string
	})

	for _, ng := range location.Spec.NodeGroups {
		for _, node := range ng.Nodes {
			if _, found := uniqueHosts[node.Name]; found {
				return nil, fmt.Errorf("duplicate node name %s found in location %s, previously defined in node group %s and in %s. Node Names must be unique across a location", node.Name, location.Name, uniqueHosts[node.Name].groupName, ng.Name)
			}
			uniqueHosts[node.Name] = struct {
				groupName string
			}{
				groupName: ng.Name,
			}
		}
	}

	return nil, nil
}
