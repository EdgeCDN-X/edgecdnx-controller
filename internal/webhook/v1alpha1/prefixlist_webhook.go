/*
This software is licensed with GNU AGPLv3.
Owner TboTech s.r.o., Slovakia
Part of EdgeCDN-X
*/

package v1alpha1

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var prefixlistlog = logf.Log.WithName("prefixlist-resource")

// SetupPrefixListWebhookWithManager registers the webhook for PrefixList in the manager.
func SetupPrefixListWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1alpha1.PrefixList{}).
		WithValidator(&PrefixListCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-infrastructure-edgecdnx-com-v1alpha1-prefixlist,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=prefixlists,verbs=create;update,versions=v1alpha1,name=vprefixlist-v1alpha1.kb.io,admissionReviewVersions=v1

// PrefixListCustomValidator struct is responsible for validating the PrefixList resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type PrefixListCustomValidator struct {
	client client.Client
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type PrefixList.
func (v *PrefixListCustomValidator) ValidateCreate(ctx context.Context, obj *infrastructurev1alpha1.PrefixList) (admission.Warnings, error) {
	prefixlist := obj
	prefixlistlog.Info("Validation for PrefixList upon creation", "name", prefixlist.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type PrefixList.
func (v *PrefixListCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *infrastructurev1alpha1.PrefixList) (admission.Warnings, error) {
	prefixlist := newObj
	prefixlistlog.Info("Validation for PrefixList upon update", "name", prefixlist.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type PrefixList.
func (v *PrefixListCustomValidator) ValidateDelete(ctx context.Context, obj *infrastructurev1alpha1.PrefixList) (admission.Warnings, error) {
	prefixlist := obj
	prefixlistlog.Info("Validation for PrefixList upon deletion", "name", prefixlist.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
