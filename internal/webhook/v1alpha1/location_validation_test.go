package v1alpha1

import (
	"context"
	"testing"

	api "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLocationBaseDomainValidation(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &corev1.LocalObjectReference{Name: "platform"}
	zones := []*api.Zone{
		{ObjectMeta: metav1.ObjectMeta{Name: "owned", Namespace: "platform"},
			Spec: api.ZoneSpec{Project: project, Zone: "example.com"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "platform"},
			Spec: api.ZoneSpec{Project: &corev1.LocalObjectReference{Name: "other"}, Zone: "foreign.net"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "other"},
			Spec: api.ZoneSpec{Project: project, Zone: "remote.net"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(zones[0], zones[1], zones[2]).Build()
	validator := &LocationCustomValidator{Client: c}
	for _, tc := range []struct {
		domain string
		valid  bool
	}{
		{"", true}, {"example.com", true}, {"fra.edge.example.com", true},
		{"edge.example.com.", true}, {"notexample.com", false},
		{"foreign.net", false}, {"remote.net", false},
	} {
		t.Run(tc.domain, func(t *testing.T) {
			location := &api.Location{ObjectMeta: metav1.ObjectMeta{Name: "fra", Namespace: "platform"},
				Spec: api.LocationSpec{Project: project, BaseDomain: tc.domain,
					Sharing: &api.ResourceSharing{AllProjects: true}}}
			_, createErr := validator.ValidateCreate(context.Background(), location)
			_, updateErr := validator.ValidateUpdate(context.Background(), location.DeepCopy(), location)
			if (createErr == nil) != tc.valid || (updateErr == nil) != tc.valid {
				t.Fatalf("valid=%t, create=%v, update=%v", tc.valid, createErr, updateErr)
			}
		})
	}
}
