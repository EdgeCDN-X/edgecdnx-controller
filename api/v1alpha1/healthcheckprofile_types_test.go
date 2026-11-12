package v1alpha1

import (
	"encoding/json"
	"os"
	"testing"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"sigs.k8s.io/yaml"
)

func TestHealthCheckProfileThresholdSchema(t *testing.T) {
	data, err := os.ReadFile("../../config/crd/bases/infrastructure.edgecdnx.com_healthcheckprofiles.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	var props apiextensions.JSONSchemaProps
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(crd.Spec.Versions[0].Schema.OpenAPIV3Schema, &props, nil); err != nil {
		t.Fatal(err)
	}
	structural, err := schema.NewStructural(&props)
	if err != nil {
		t.Fatal(err)
	}
	validator, _, err := validation.NewSchemaValidator(&props)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"successThreshold", "failureThreshold"} {
		t.Run(field, func(t *testing.T) {
			// Old manifests omit both settings; API-server defaulting must supply 1.
			object := map[string]interface{}{
				"spec": map[string]interface{}{
					"project": map[string]interface{}{"name": "tenant"},
					"probes": []interface{}{map[string]interface{}{
						"name": "static", "type": "ASSUME",
						"assume": map[string]interface{}{"status": "Healthy"},
					}},
				},
			}
			defaulting.Default(object, structural)
			spec := object["spec"].(map[string]interface{})
			defaultedJSON, err := json.Marshal(spec)
			if err != nil {
				t.Fatal(err)
			}
			var defaulted HealthCheckProfileSpec
			if err := json.Unmarshal(defaultedJSON, &defaulted); err != nil {
				t.Fatal(err)
			}
			if defaulted.SuccessThreshold != 1 || defaulted.FailureThreshold != 1 {
				t.Fatalf("missing legacy defaults: %+v", spec)
			}
			for _, value := range []float64{-1, 0, 0.5, 1, 3} {
				spec[field] = value
				problems := validation.ValidateCustomResource(nil, object, validator)
				if (len(problems) == 0) != (value >= 1) {
					t.Errorf("%s=%v: validation errors=%v", field, value, problems)
				}
			}
		})
	}
}

func TestHealthCheckProfileThresholdsOmitEmpty(t *testing.T) {
	data, err := json.Marshal(HealthCheckProfileSpec{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]interface{}
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"successThreshold", "failureThreshold"} {
		if _, exists := object[field]; exists {
			t.Errorf("zero-valued %s must be omitted", field)
		}
	}
}
