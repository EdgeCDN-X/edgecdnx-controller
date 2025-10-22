package builder

import (
	"crypto/md5"
	"encoding/json"
	"fmt"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const ValuesHashAnnotation = "edgedcnx.com/values-hash"

type ThrowerOptions struct {
	ThrowerChartName       string
	ThrowerChartVersion    string
	ThrowerChartRepository string
	TargetNamespace        string
	ApplicationSetProject  string
}

type ChartParams struct {
	ChartRepository string
	ChartName       string
	ChartVersion    string
	ReleaseName     string
}

type IAppsetBuilder interface {
	SetHelmChartParams(params ChartParams)
	SetHelmValues(any)
	SetTargetAppMeta(templatedName string, namespace string, targetNamespace string)
	SetProject(project string)
	SetLabelMatch([][]metav1.LabelSelectorRequirement)
	SetAppsetMeta(name string, namespace string)
	Build() (argoprojv1alpha1.ApplicationSet, string, error)
}

type ThrowableAppsetBuilder struct {
	appSet argoprojv1alpha1.ApplicationSet
}

func newThrowableAppsetBuilder(name string) *ThrowableAppsetBuilder {
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

func (b *ThrowableAppsetBuilder) SetTargetAppMeta(templatedName string, namespace string, targetNamespace string) {
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

func (b *ThrowableAppsetBuilder) SetAppsetMeta(name string, namespace string) {
	b.appSet.ObjectMeta.Name = name
	b.appSet.ObjectMeta.Namespace = namespace
}

func (b *ThrowableAppsetBuilder) Build() (argoprojv1alpha1.ApplicationSet, string, error) {
	marshalled, err := json.Marshal(b.appSet)
	if err != nil {
		return argoprojv1alpha1.ApplicationSet{}, "", err
	}

	hash := fmt.Sprintf("%x", md5.Sum(marshalled))

	b.appSet.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("appset-builder")
	logger.V(1).Info("Built ApplicationSet", "name", b.appSet.Name, "namespace", b.appSet.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.appSet, hash, nil
}

func AppsetBuilderFactory(builderType string, name string, namespace string, throwerOption ThrowerOptions) (IAppsetBuilder, error) {
	switch builderType {
	case "Location":
		b := newThrowableAppsetBuilder(name)
		b.SetHelmChartParams(ChartParams{
			ChartRepository: throwerOption.ThrowerChartRepository,
			ChartName:       throwerOption.ThrowerChartName,
			ChartVersion:    throwerOption.ThrowerChartVersion,
			ReleaseName:     `location-{{ name }}`,
		})
		b.SetTargetAppMeta(fmt.Sprintf(`location-%s-at-{{ name }}`, name), namespace, throwerOption.TargetNamespace)
		b.SetProject(throwerOption.ApplicationSetProject)
		b.SetAppsetMeta(name, namespace)
		b.SetLabelMatch([][]metav1.LabelSelectorRequirement{
			{
				{
					Key:      "edgecdnx.com/routing",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"true", "yes"},
				},
			},
		})
		return b, nil
	case "Challenge":
		b := newThrowableAppsetBuilder(name)
		b.SetHelmChartParams(ChartParams{
			ChartRepository: throwerOption.ThrowerChartRepository,
			ChartName:       throwerOption.ThrowerChartName,
			ChartVersion:    throwerOption.ThrowerChartVersion,
			ReleaseName:     `challenge-{{ name }}`,
		})
		b.SetTargetAppMeta(fmt.Sprintf(`challenge-%s-at-{{ name }}`, name), namespace, throwerOption.TargetNamespace)
		b.SetProject(throwerOption.ApplicationSetProject)
		b.SetAppsetMeta(name, namespace)
		b.SetLabelMatch([][]metav1.LabelSelectorRequirement{
			{
				{
					Key:      "edgecdnx.com/caching",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"true", "yes"},
				},
			},
		})
		return b, nil
	default:
		return nil, fmt.Errorf("unknown builder type: %s", builderType)
	}
}
