package throwable

import (
	"crypto/md5"
	"encoding/json"
	"fmt"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
)

type ThrowerHelmValues struct {
	Resources []any `json:"resources,omitempty"`
}

func (t ThrowerHelmValues) GetMd5Hash() (string, error) {
	valuesObject, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("failed to marshal values object: %w", err)
	}
	return fmt.Sprintf("%x", md5.Sum(valuesObject)), nil
}

func (t ThrowerHelmValues) GetAppSetSpec(chartRepository string, chart string, chartVersion string, appsetNamespace string, project string, targetNamespace string, name string, labelMatch []metav1.LabelSelectorRequirement) (argoprojv1alpha1.ApplicationSetSpec, error) {
	valuesObject, err := json.Marshal(t)
	if err != nil {
		return argoprojv1alpha1.ApplicationSetSpec{}, fmt.Errorf("failed to marshal values object: %w", err)
	}

	var generators []argoprojv1alpha1.ApplicationSetGenerator
	if len(labelMatch) == 0 {
		generators = append(generators, argoprojv1alpha1.ApplicationSetGenerator{
			Clusters: &argoprojv1alpha1.ClusterGenerator{
				Values: map[string]string{
					"chartRepository": chartRepository,
					"chart":           chart,
					"chartVersion":    chartVersion,
				},
			},
		})
	} else {
		for _, match := range labelMatch {
			generators = append(generators, argoprojv1alpha1.ApplicationSetGenerator{
				Clusters: &argoprojv1alpha1.ClusterGenerator{
					Selector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{match},
					},

					Values: map[string]string{
						"chartRepository": chartRepository,
						"chart":           chart,
						"chartVersion":    chartVersion,
					},
				},
			})
		}
	}

	return argoprojv1alpha1.ApplicationSetSpec{
		Generators: generators,
		Template: argoprojv1alpha1.ApplicationSetTemplate{
			ApplicationSetTemplateMeta: argoprojv1alpha1.ApplicationSetTemplateMeta{
				Name:      name,
				Namespace: appsetNamespace,
			},
			Spec: argoprojv1alpha1.ApplicationSpec{
				Project: project,
				Destination: argoprojv1alpha1.ApplicationDestination{
					Server:    "{{ server }}",
					Namespace: targetNamespace,
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
	}, nil
}
