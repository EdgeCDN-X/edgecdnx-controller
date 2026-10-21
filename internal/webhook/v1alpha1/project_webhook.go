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

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/tenancy"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var projectlog = logf.Log.WithName("project-resource")

// SetupProjectWebhookWithManager registers the webhook for Project in the manager.
func SetupProjectWebhookWithManager(mgr ctrl.Manager, scope tenancy.Scope) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1alpha1.Project{}).
		WithValidator(&ProjectCustomValidator{Scope: scope}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-infrastructure-edgecdnx-com-v1alpha1-project,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=projects,verbs=create;update,versions=v1alpha1,name=vproject-v1alpha1.kb.io,admissionReviewVersions=v1

// ProjectCustomValidator struct is responsible for validating the Project resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ProjectCustomValidator struct {
	Scope tenancy.Scope
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Project.
func (v *ProjectCustomValidator) ValidateCreate(ctx context.Context, obj *infrastructurev1alpha1.Project) (admission.Warnings, error) {
	projectlog.Info("Validation for Project upon creation", "name", obj.GetName())

	return nil, v.Scope.ValidateProject(obj.Namespace, obj.Name)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Project.
func (v *ProjectCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *infrastructurev1alpha1.Project) (admission.Warnings, error) {
	projectlog.Info("Validation for Project upon update", "name", newObj.GetName())

	return nil, v.Scope.ValidateProject(newObj.Namespace, newObj.Name)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Project.
func (v *ProjectCustomValidator) ValidateDelete(ctx context.Context, obj *infrastructurev1alpha1.Project) (admission.Warnings, error) {
	projectlog.Info("Validation for Project upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
