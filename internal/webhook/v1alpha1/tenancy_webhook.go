package v1alpha1

import (
	"context"
	"net/http"

	"github.com/EdgeCDN-X/edgecdnx-controller/internal/tenancy"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-tenancy,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.edgecdnx.com,resources=projects;apikeys;dnsendpoints;healthcheckprofiles;locations;prefixlists;services;zones,verbs=create;update,versions=v1alpha1,name=tenancy.infrastructure.edgecdnx.com,admissionReviewVersions=v1
// +kubebuilder:rbac:groups=infrastructure.edgecdnx.com,resources=projects;apikeys;dnsendpoints;healthcheckprofiles;locations;prefixlists;services;zones,verbs=get;list;watch

func SetupTenancyWebhookWithManager(mgr ctrl.Manager, scope tenancy.Scope) {
	mgr.GetWebhookServer().Register("/validate-tenancy", &admission.Webhook{Handler: &tenancyHandler{
		scope: scope, reader: mgr.GetAPIReader(), decoder: serializer.NewCodecFactory(mgr.GetScheme()).UniversalDeserializer(),
	}})
}

type tenancyHandler struct {
	scope   tenancy.Scope
	reader  client.Reader
	decoder runtime.Decoder
}

func (h *tenancyHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	if request.Operation != admissionv1.Create && request.Operation != admissionv1.Update {
		return admission.Allowed("")
	}
	decoded, _, err := h.decoder.Decode(request.Object.Raw, nil, nil)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	object, ok := decoded.(client.Object)
	if !ok {
		return admission.Denied("expected a Kubernetes resource")
	}
	var oldObject client.Object
	if request.Operation == admissionv1.Update {
		decoded, _, err = h.decoder.Decode(request.OldObject.Raw, nil, nil)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		oldObject, ok = decoded.(client.Object)
		if !ok {
			return admission.Denied("expected an old Kubernetes resource")
		}
	}
	if err := h.scope.ValidateResource(ctx, h.reader, object, oldObject); err != nil {
		return admission.Denied(err.Error())
	}
	return admission.Allowed("")
}
