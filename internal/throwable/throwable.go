package throwable

import (
	"crypto/md5"
	"encoding/json"
	"fmt"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
)

type AppsetSpecOptions struct {
	ChartRepository string
	Chart           string
	ChartVersion    string
	AppsetNamespace string
	Project         string
	TargetNamespace string
	Name            string
	LabelMatch      [][]metav1.LabelSelectorRequirement
}

type ThrowerHelmValues struct {
	Resources []any `json:"resources,omitempty"`
}

func (t ThrowerHelmValues) GetAppSetSpec(Options AppsetSpecOptions) (argoprojv1alpha1.ApplicationSetSpec, map[string]string, string, error) {
	valuesObject, err := json.Marshal(t)
	if err != nil {
		return argoprojv1alpha1.ApplicationSetSpec{}, make(map[string]string), "", fmt.Errorf("failed to marshal values object: %w", err)
	}

	var generators []argoprojv1alpha1.ApplicationSetGenerator
	for g := range Options.LabelMatch {
		generators = append(generators, argoprojv1alpha1.ApplicationSetGenerator{
			Clusters: &argoprojv1alpha1.ClusterGenerator{
				Selector: metav1.LabelSelector{
					MatchExpressions: Options.LabelMatch[g],
				},
				Values: map[string]string{
					"chartRepository": Options.ChartRepository,
					"chart":           Options.Chart,
					"chartVersion":    Options.ChartVersion,
				},
			},
		})
	}

	marshable := struct {
		Spec        argoprojv1alpha1.ApplicationSetSpec `json:"spec"`
		Annotations map[string]string                   `json:"annotations"`
	}{
		Spec: argoprojv1alpha1.ApplicationSetSpec{
			Generators: generators,
			Template: argoprojv1alpha1.ApplicationSetTemplate{
				ApplicationSetTemplateMeta: argoprojv1alpha1.ApplicationSetTemplateMeta{
					Name:      Options.Name,
					Namespace: Options.AppsetNamespace,
				},
				Spec: argoprojv1alpha1.ApplicationSpec{
					Project: Options.Project,
					Destination: argoprojv1alpha1.ApplicationDestination{
						Server:    "{{ server }}",
						Namespace: Options.TargetNamespace,
					},
					Sources: []argoprojv1alpha1.ApplicationSource{
						{
							Chart:          "{{ values.chart }}",
							RepoURL:        "{{ values.chartRepository }}",
							TargetRevision: "{{ values.chartVersion }}",
							Helm: &argoprojv1alpha1.ApplicationSourceHelm{
								ReleaseName: "{{ name }}",
								ValuesObject: &runtime.RawExtension{
									Raw: valuesObject,
								},
							},
						},
					},
					SyncPolicy: &argoprojv1alpha1.SyncPolicy{
						Automated: &argoprojv1alpha1.SyncPolicyAutomated{
							Prune:      true,
							SelfHeal:   true,
							AllowEmpty: true,
						},
						SyncOptions: []string{"CreateNamespace=true"},
					},
				},
			},
		},
		Annotations: map[string]string{},
	}

	hashable, err := json.Marshal(marshable)
	if err != nil {
		return argoprojv1alpha1.ApplicationSetSpec{}, make(map[string]string), "", fmt.Errorf("failed to marshal values object: %w", err)
	}
	return marshable.Spec, marshable.Annotations, fmt.Sprintf("%x", md5.Sum(hashable)), nil
}
