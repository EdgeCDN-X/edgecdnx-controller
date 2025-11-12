package builder

import (
	"crypto/md5"
	"encoding/json"
	"fmt"

	argoprojv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ResourceTypeLabel        = "edgecdnx.com/resource-type"
	MonitoringTLSSecretValue = "monitoring-tls"
	AnalyticsTLSSecretValue  = "analytics-tls"
)

type IAppBuilder interface {
	WithHelmChartParams(params ChartParams)
	WithHelmValues(any)
	WithProject(project string)
	WithAppMeta(name string, namespace string)
	WithDestination(destination string, namespace string)
	Build() (argoprojv1alpha1.Application, string, error)
}

type ThrowableAppBuilder struct {
	app argoprojv1alpha1.Application
}

func NewThrowableAppBuilder(name string) *ThrowableAppBuilder {
	return &ThrowableAppBuilder{
		app: argoprojv1alpha1.Application{
			Spec: argoprojv1alpha1.ApplicationSpec{
				Project: "default",
				Destination: argoprojv1alpha1.ApplicationDestination{
					Name:      "",
					Namespace: "",
				},
				Source: &argoprojv1alpha1.ApplicationSource{},
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
	}
}

func (b *ThrowableAppBuilder) WithHelmChartParams(params ChartParams) {
	b.app.Spec.Source.Chart = params.ChartName
	b.app.Spec.Source.RepoURL = params.ChartRepository
	b.app.Spec.Source.TargetRevision = params.ChartVersion
	b.app.Spec.Source.Helm = &argoprojv1alpha1.ApplicationSourceHelm{
		ReleaseName: params.ReleaseName,
	}
}

func (b *ThrowableAppBuilder) WithHelmValues(values any) {
	valuesObject, _ := json.Marshal(values)

	b.app.Spec.Source.Helm.ValuesObject = &runtime.RawExtension{
		Raw: valuesObject,
	}
}

func (b *ThrowableAppBuilder) WithProject(project string) {
	b.app.Spec.Project = project
}

func (b *ThrowableAppBuilder) WithAppMeta(name string, namespace string) {
	b.app.ObjectMeta.Name = name
	b.app.ObjectMeta.Namespace = namespace
	b.app.ObjectMeta.Finalizers = []string{"resources-finalizer.argocd.argoproj.io"}
}

func (b *ThrowableAppBuilder) WithDestination(destination string, namespace string) {
	b.app.Spec.Destination.Server = destination
	b.app.Spec.Destination.Namespace = namespace
}

func (b *ThrowableAppBuilder) Build() (argoprojv1alpha1.Application, string, error) {

	marshalled, err := json.Marshal(b.app)
	if err != nil {
		return argoprojv1alpha1.Application{}, "", err
	}

	hash := fmt.Sprintf("%x", md5.Sum(marshalled))

	b.app.SetAnnotations(map[string]string{
		ValuesHashAnnotation: hash,
	})

	logger := logf.Log.WithName("appset-builder")
	logger.V(1).Info("Built Application", "name", b.app.Name, "namespace", b.app.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.app, hash, nil
}

func AppBuilderFactory(builderType string, name string, namespace string, destionation string, throwerOptions ThrowerOptions) (IAppBuilder, error) {
	switch builderType {
	case MonitoringTLSSecretValue:
		b := NewThrowableAppBuilder(name)
		// Reuse for apps too
		b.WithProject(throwerOptions.ApplicationSetProject)
		b.WithHelmChartParams(ChartParams{
			ChartRepository: throwerOptions.ThrowerChartRepository,
			ChartName:       throwerOptions.ThrowerChartName,
			ChartVersion:    throwerOptions.ThrowerChartVersion,
			ReleaseName:     name,
		})
		b.WithDestination(destionation, namespace)
		b.WithAppMeta(name, namespace)
		return b, nil
	case AnalyticsTLSSecretValue:
		b := NewThrowableAppBuilder(name)
		// Reuse for apps too
		b.WithProject(throwerOptions.ApplicationSetProject)
		b.WithHelmChartParams(ChartParams{
			ChartRepository: throwerOptions.ThrowerChartRepository,
			ChartName:       throwerOptions.ThrowerChartName,
			ChartVersion:    throwerOptions.ThrowerChartVersion,
			ReleaseName:     name,
		})
		b.WithDestination(destionation, namespace)
		b.WithAppMeta(name, namespace)
		return b, nil
	}
	return nil, fmt.Errorf("unknown builder type: %s", builderType)
}
