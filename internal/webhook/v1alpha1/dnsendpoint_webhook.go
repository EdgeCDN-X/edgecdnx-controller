/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package v1alpha1

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
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

const maxDNSRecordTTL = 1<<31 - 1

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
	if !validDNSName(zone.Spec.Zone, false) {
		return nil, fmt.Errorf("zone %q has an invalid DNS name", zone.Spec.Zone)
	}
	if !validDNSName(dnsendpoint.Spec.DNSName, true) {
		return nil, fmt.Errorf("spec.dnsName %q is not a valid DNS name", dnsendpoint.Spec.DNSName)
	}
	if !dns.IsSubDomain(dns.Fqdn(zone.Spec.Zone), dns.Fqdn(dnsendpoint.Spec.DNSName)) {
		return nil, fmt.Errorf("spec.dnsName %q is outside zone %q", dnsendpoint.Spec.DNSName, zone.Spec.Zone)
	}
	if dnsendpoint.Spec.RecordTTL < 0 || dnsendpoint.Spec.RecordTTL > maxDNSRecordTTL {
		return nil, fmt.Errorf("spec.recordTTL must be between 0 and %d", maxDNSRecordTTL)
	}
	if dns.StringToType[strings.ToUpper(dnsendpoint.Spec.RecordType)] == 0 || !supportedDNSRecordType(dnsendpoint.Spec.RecordType) {
		return nil, fmt.Errorf("spec.recordType %q is not supported", dnsendpoint.Spec.RecordType)
	}
	if !isSimpleRouting(dnsendpoint.Spec.RoutingPolicy) && !isAddressRecordType(dnsendpoint.Spec.RecordType) {
		return nil, fmt.Errorf("spec.recordType %q is not supported with routingPolicy %q", dnsendpoint.Spec.RecordType, dnsendpoint.Spec.RoutingPolicy)
	}
	if err := validateDNSRecordTargets(dnsendpoint); err != nil {
		return nil, err
	}

	endpoints := &infrastructurev1alpha1.DNSEndpointList{}
	if err := v.Client.List(ctx, endpoints, client.InNamespace(dnsendpoint.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}
	if err := validateDNSRecordConflicts(dnsendpoint, endpoints.Items); err != nil {
		return nil, err
	}

	return nil, nil
}

func validDNSName(name string, allowWildcard bool) bool {
	name = strings.TrimSuffix(name, ".")
	if name == "" || name == "." {
		return false
	}
	labels := strings.Split(name, ".")
	for index, label := range labels {
		if strings.Contains(label, "*") && (!allowWildcard || index != 0 || label != "*") {
			return false
		}
	}
	_, valid := dns.IsDomainName(dns.Fqdn(name))
	return valid
}

func supportedDNSRecordType(recordType string) bool {
	switch strings.ToUpper(recordType) {
	case "A", "AAAA", "CNAME", "TXT", "MX", "SRV", "NS":
		return true
	default:
		return false
	}
}

func isAddressRecordType(recordType string) bool {
	return strings.EqualFold(recordType, "A") || strings.EqualFold(recordType, "AAAA")
}

func validateDNSRecordTargets(endpoint *infrastructurev1alpha1.DNSEndpoint) error {
	policy := strings.ToLower(endpoint.Spec.RoutingPolicy)
	if policy == "" {
		policy = "simple"
	}
	if policy != "simple" {
		return nil
	}
	if len(endpoint.Spec.Targets) == 0 {
		return fmt.Errorf("spec.targets must contain at least one target for Simple routing")
	}
	if strings.EqualFold(endpoint.Spec.RecordType, "CNAME") && len(endpoint.Spec.Targets) != 1 {
		return fmt.Errorf("CNAME records must have exactly one target")
	}
	for _, target := range endpoint.Spec.Targets {
		if strings.EqualFold(endpoint.Spec.RecordType, "CNAME") && net.ParseIP(strings.TrimSuffix(target, ".")) != nil {
			return fmt.Errorf("CNAME target %q must be a domain name, not an IP address", target)
		}
		recordText := fmt.Sprintf("%s %d IN %s %s", dns.Fqdn(endpoint.Spec.DNSName), endpoint.Spec.RecordTTL, strings.ToUpper(endpoint.Spec.RecordType), target)
		record, err := dns.NewRR(recordText)
		if err != nil {
			return fmt.Errorf("invalid target %q for %s record: %w", target, endpoint.Spec.RecordType, err)
		}
		if strings.EqualFold(endpoint.Spec.RecordType, "CNAME") {
			cname := record.(*dns.CNAME)
			if canonicalDNSName(cname.Target) == canonicalDNSName(endpoint.Spec.DNSName) {
				return fmt.Errorf("CNAME record target must differ from its owner name")
			}
		}
	}
	return nil
}

func validateDNSRecordConflicts(endpoint *infrastructurev1alpha1.DNSEndpoint, existing []infrastructurev1alpha1.DNSEndpoint) error {
	owner := canonicalDNSName(endpoint.Spec.DNSName)
	recordType := strings.ToUpper(endpoint.Spec.RecordType)
	zoneName := endpoint.Spec.Zone.Name

	for index := range existing {
		other := &existing[index]
		if other.Namespace == endpoint.Namespace && other.Name == endpoint.Name {
			continue
		}
		otherOwner := canonicalDNSName(other.Spec.DNSName)
		otherType := strings.ToUpper(other.Spec.RecordType)
		sameOwner := owner == otherOwner
		sameZone := other.Spec.Zone != nil && other.Spec.Zone.Name == zoneName

		if sameOwner && sameZone && recordType == otherType {
			return fmt.Errorf("DNS record %q type %s already exists in zone %q", endpoint.Spec.DNSName, recordType, zoneName)
		}
		if sameOwner && (recordType == "CNAME" || otherType == "CNAME") {
			return fmt.Errorf("CNAME record %q cannot coexist with another record at the same owner name", endpoint.Spec.DNSName)
		}

		if recordType == "CNAME" && isSimpleRouting(other.Spec.RoutingPolicy) && (otherType == "MX" || otherType == "NS") {
			for _, target := range mxOrNSTargets(other) {
				if canonicalDNSName(target) == owner {
					return fmt.Errorf("%s target %q cannot be a CNAME", otherType, endpoint.Spec.DNSName)
				}
			}
		}
		if isSimpleRouting(endpoint.Spec.RoutingPolicy) && (recordType == "MX" || recordType == "NS") {
			for _, target := range endpoint.Spec.Targets {
				targetName := targetNameFromTarget(recordType, target)
				if otherType == "CNAME" && canonicalDNSName(targetName) == otherOwner {
					return fmt.Errorf("%s target %q cannot be a CNAME", recordType, targetName)
				}
			}
		}
	}
	return nil
}

func mxOrNSTargets(endpoint *infrastructurev1alpha1.DNSEndpoint) []string {
	if !isSimpleRouting(endpoint.Spec.RoutingPolicy) {
		return nil
	}
	targets := make([]string, 0, len(endpoint.Spec.Targets))
	for _, target := range endpoint.Spec.Targets {
		recordText := fmt.Sprintf("%s %d IN %s %s", dns.Fqdn(endpoint.Spec.DNSName), endpoint.Spec.RecordTTL, strings.ToUpper(endpoint.Spec.RecordType), target)
		record, err := dns.NewRR(recordText)
		if err != nil {
			continue
		}
		switch typedRecord := record.(type) {
		case *dns.MX:
			targets = append(targets, typedRecord.Mx)
		case *dns.NS:
			targets = append(targets, typedRecord.Ns)
		}
	}
	return targets
}

func targetNameFromTarget(recordType, target string) string {
	recordText := fmt.Sprintf("target.invalid. 0 IN %s %s", recordType, target)
	record, err := dns.NewRR(recordText)
	if err != nil {
		return ""
	}
	switch typedRecord := record.(type) {
	case *dns.MX:
		return typedRecord.Mx
	case *dns.NS:
		return typedRecord.Ns
	default:
		return ""
	}
}

func canonicalDNSName(name string) string {
	return strings.ToLower(dns.Fqdn(strings.TrimSuffix(name, ".")))
}

func isSimpleRouting(routingPolicy string) bool {
	return routingPolicy == "" || strings.EqualFold(routingPolicy, "Simple")
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
