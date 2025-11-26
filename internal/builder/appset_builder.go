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

type IAppsetBuilder interface {
	WithHelmChartParams(params ChartParams)
	WithHelmValues(any)
	WithTargetAppMeta(templatedName string, namespace string, targetNamespace string)
	WithProject(project string)
	WithLabelMatchers([][]metav1.LabelSelectorRequirement)
	WithAppsetMeta(name string, namespace string)
	Build() (argoprojv1alpha1.ApplicationSet, string, error)
}

type ThrowableAppsetBuilder struct {
	appSet argoprojv1alpha1.ApplicationSet
}

func NewThrowableAppsetBuilder(name string) *ThrowableAppsetBuilder {
	return &ThrowableAppsetBuilder{
		appSet: argoprojv1alpha1.ApplicationSet{
			TypeMeta: metav1.TypeMeta{
				APIVersion: argoprojv1alpha1.SchemeGroupVersion.String(),
				Kind:       "ApplicationSet",
			},
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

func (b *ThrowableAppsetBuilder) WithHelmChartParams(params ChartParams) {
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

func (b *ThrowableAppsetBuilder) WithHelmValues(values any) {
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

func (b *ThrowableAppsetBuilder) WithTargetAppMeta(templatedName string, namespace string, targetNamespace string) {
	b.appSet.Spec.Template.Name = templatedName
	b.appSet.Spec.Template.Namespace = namespace
	b.appSet.Spec.Template.Spec.Destination.Namespace = targetNamespace
	b.appSet.Spec.Template.Spec.Destination.Server = "{{ server }}"
}

func (b *ThrowableAppsetBuilder) WithProject(project string) {
	b.appSet.Spec.Template.Spec.Project = project
}

func (b *ThrowableAppsetBuilder) WithLabelMatchers(labelMatch [][]metav1.LabelSelectorRequirement) {
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

func (b *ThrowableAppsetBuilder) WithAppsetMeta(name string, namespace string) {
	b.appSet.Name = name
	b.appSet.Namespace = namespace
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
		b := NewThrowableAppsetBuilder(name)
		b.WithHelmChartParams(ChartParams{
			ChartRepository: throwerOption.ThrowerChartRepository,
			ChartName:       throwerOption.ThrowerChartName,
			ChartVersion:    throwerOption.ThrowerChartVersion,
			ReleaseName:     `location-{{ name }}`,
		})
		b.WithTargetAppMeta(fmt.Sprintf(`location-%s-at-{{ name }}`, name), namespace, throwerOption.TargetNamespace)
		b.WithProject(throwerOption.ApplicationSetProject)
		b.WithAppsetMeta(name, namespace)
		b.WithLabelMatchers([][]metav1.LabelSelectorRequirement{
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
		b := NewThrowableAppsetBuilder(name)
		b.WithHelmChartParams(ChartParams{
			ChartRepository: throwerOption.ThrowerChartRepository,
			ChartName:       throwerOption.ThrowerChartName,
			ChartVersion:    throwerOption.ThrowerChartVersion,
			ReleaseName:     `challenge-{{ name }}`,
		})
		b.WithTargetAppMeta(fmt.Sprintf(`challenge-%s-at-{{ name }}`, name), namespace, throwerOption.TargetNamespace)
		b.WithProject(throwerOption.ApplicationSetProject)
		b.WithAppsetMeta(name, namespace)
		b.WithLabelMatchers([][]metav1.LabelSelectorRequirement{
			{
				{
					Key:      "edgecdnx.com/caching",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"true", "yes"},
				},
			},
		})
		return b, nil
	case "PrefixList":
		b := NewThrowableAppsetBuilder(name)
		b.WithHelmChartParams(ChartParams{
			ChartRepository: throwerOption.ThrowerChartRepository,
			ChartName:       throwerOption.ThrowerChartName,
			ChartVersion:    throwerOption.ThrowerChartVersion,
			ReleaseName:     `prefixlist-{{ name }}`,
		})
		b.WithTargetAppMeta(fmt.Sprintf(`prefixlist-%s-at-{{ name }}`, name), namespace, throwerOption.TargetNamespace)
		b.WithProject(throwerOption.ApplicationSetProject)
		b.WithAppsetMeta(name, namespace)
		b.WithLabelMatchers([][]metav1.LabelSelectorRequirement{
			{
				{
					Key:      "edgecdnx.com/routing",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"true", "yes"},
				},
			},
		})
		return b, nil
	case "Service":
		b := NewThrowableAppsetBuilder(name)
		b.WithHelmChartParams(ChartParams{
			ChartRepository: throwerOption.ThrowerChartRepository,
			ChartName:       throwerOption.ThrowerChartName,
			ChartVersion:    throwerOption.ThrowerChartVersion,
			ReleaseName:     `service-{{ name }}`,
		})
		b.WithTargetAppMeta(fmt.Sprintf(`service-%s-at-{{ name }}`, name), namespace, throwerOption.TargetNamespace)
		b.WithProject(throwerOption.ApplicationSetProject)
		b.WithAppsetMeta(name, namespace)
		b.WithLabelMatchers([][]metav1.LabelSelectorRequirement{
			{
				{
					Key:      "edgecdnx.com/routing",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"true", "yes"},
				},
			},
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
