package builder

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const ValuesHashAnnotation = "edgedcnx.com/values-hash"

type ChartParams struct {
	ChartRepository string
	ChartName       string
	ChartVersion    string
	ReleaseName     string
}

type IAppsetBuilder interface {
	SetHelmChartParams(params ChartParams)
	SetHelmValues(any)
	SetTargetMeta(templatedName string, namespace string, targetNamespace string)
	SetProject(project string)
	SetLabelMatch([][]metav1.LabelSelectorRequirement)
	Build(name string, namespace string) error
}

type ThrowableAppsetBuilder struct {
	appSet argoprojv1alpha1.ApplicationSet
}

func NewThrowableAppsetBuilder(name string) *ThrowableAppsetBuilder {
	return &ThrowableAppsetBuilder{
		appSet: argoprojv1alpha1.ApplicationSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
			Spec: argoprojv1alpha1.ApplicationSetSpec{
				Generators: []argoprojv1alpha1.ApplicationSetGenerator{},
				Template: argoprojv1alpha1.ApplicationSetTemplate{
					ApplicationSetTemplateMeta: argoprojv1alpha1.ApplicationSetTemplateMeta{},
					Spec: argoprojv1alpha1.ApplicationSpec{
						Project: "default",
						Sources: []argoprojv1alpha1.ApplicationSource{},
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
		},
	}
}

// Sets chart params
func (b *ThrowableAppsetBuilder) SetHelmChartParams(params ChartParams) {
	if len(b.appSet.Spec.Template.Spec.Sources) == 0 {
		b.appSet.Spec.Template.Spec.Sources = append(b.appSet.Spec.Template.Spec.Sources, argoprojv1alpha1.ApplicationSource{
			Chart:          params.ChartName,
			RepoURL:        params.ChartRepository,
			TargetRevision: params.ChartVersion,
			Helm: &argoprojv1alpha1.ApplicationSourceHelm{
				ReleaseName: params.ReleaseName,
			},
		})
	} else {
		b.appSet.Spec.Template.Spec.Sources[0].Chart = params.ChartName
		b.appSet.Spec.Template.Spec.Sources[0].RepoURL = params.ChartRepository
		b.appSet.Spec.Template.Spec.Sources[0].TargetRevision = params.ChartVersion
		b.appSet.Spec.Template.Spec.Sources[0].Helm.ReleaseName = params.ReleaseName
	}
}

// Set helm values
func (b *ThrowableAppsetBuilder) SetHelmValues(values any) {
	valuesObject, _ := json.Marshal(values)

	if len(b.appSet.Spec.Template.Spec.Sources) == 0 {
		b.appSet.Spec.Template.Spec.Sources = append(b.appSet.Spec.Template.Spec.Sources, argoprojv1alpha1.ApplicationSource{
			Helm: &argoprojv1alpha1.ApplicationSourceHelm{
				ValuesObject: &runtime.RawExtension{
					Raw: valuesObject,
				},
			},
		})
	} else {
		b.appSet.Spec.Template.Spec.Sources[0].Helm.ValuesObject = &runtime.RawExtension{
			Raw: valuesObject,
		}
	}
}

func (b *ThrowableAppsetBuilder) SetTargetMeta(templatedName string, namespace string, targetNamespace string) {
	b.appSet.Spec.Template.ApplicationSetTemplateMeta.Name = templatedName
	b.appSet.Spec.Template.ApplicationSetTemplateMeta.Namespace = namespace
	b.appSet.Spec.Template.Spec.Destination.Namespace = targetNamespace
	b.appSet.Spec.Template.Spec.Destination.Server = "{{ server }}"
}

func (b *ThrowableAppsetBuilder) SetProject(project string) {
	b.appSet.Spec.Template.Spec.Project = project
}

func (b *ThrowableAppsetBuilder) SetLabelMatch(labelMatch [][]metav1.LabelSelectorRequirement) {
	generators := make([]argoprojv1alpha1.ApplicationSetGenerator, 0, len(labelMatch))
	for g := range labelMatch {
		generators = append(generators, argoprojv1alpha1.ApplicationSetGenerator{
			Clusters: &argoprojv1alpha1.ClusterGenerator{
				Selector: metav1.LabelSelector{
					MatchExpressions: labelMatch[g],
				},
			},
		})
	}
	b.appSet.Spec.Generators = generators
}

func (b *ThrowableAppsetBuilder) Build(name string, namespace string) (argoprojv1alpha1.ApplicationSet, string, error) {
	b.appSet.ObjectMeta.Name = name
	b.appSet.ObjectMeta.Namespace = namespace

	marshalled, err := json.Marshal(b.appSet)
	if err != nil {
		return argoprojv1alpha1.ApplicationSet{}, "", err
	}

	hash := fmt.Sprintf("%x", md5.Sum(marshalled))

	b.appSet.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := log.Default()
	logger.Print("Built ApplicationSet", "name", name, "namespace", namespace, "hash", hash, "marshal", string(marshalled))

	return b.appSet, hash, nil
}
