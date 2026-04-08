package builder

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	infrastructurev1alpha1 "github.com/EdgeCDN-X/edgecdnx-controller/api/v1alpha1"
	monv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultLocationProbeHealthPath = "/healthz"
	LocationProbeModule            = "http_2xx"
	DefaultLocationProbeProberURL  = "blackbox-exporter:9115"
	DefaultMonitoringReleaseLabel  = "edgecdnx-monitoring"
)

type ProbeBuilderConfig struct {
	LocationProbeHealthPath string
	LocationProbeProberURL  string
	MonitoringReleaseLabel  string
}

func DefaultProbeBuilderConfig() ProbeBuilderConfig {
	return ProbeBuilderConfig{
		LocationProbeHealthPath: DefaultLocationProbeHealthPath,
		LocationProbeProberURL:  DefaultLocationProbeProberURL,
		MonitoringReleaseLabel:  DefaultMonitoringReleaseLabel,
	}
}

func (c ProbeBuilderConfig) WithDefaults() ProbeBuilderConfig {
	defaults := DefaultProbeBuilderConfig()

	if c.LocationProbeHealthPath == "" {
		c.LocationProbeHealthPath = defaults.LocationProbeHealthPath
	}

	if c.LocationProbeProberURL == "" {
		c.LocationProbeProberURL = defaults.LocationProbeProberURL
	}

	if c.MonitoringReleaseLabel == "" {
		c.MonitoringReleaseLabel = defaults.MonitoringReleaseLabel
	}

	return c
}

type IProbeBuilder interface {
	WithLabel(key string, value string)
	WithAnnotation(key string, value string)
	WithJobName(jobName string)
	WithModule(module string)
	WithProber(url string, scheme monv1.Scheme, path string)
	WithTargets(targets []string)
	WithLocation(location infrastructurev1alpha1.Location)
	Build() (monv1.Probe, string, error)
}

type ProbeBuilder struct {
	probe  monv1.Probe
	config ProbeBuilderConfig
}

func NewDefaultProbeBuilder(name string, namespace string, config ProbeBuilderConfig) *ProbeBuilder {
	return &ProbeBuilder{
		config: config.WithDefaults(),
		probe: monv1.Probe{
			TypeMeta: metav1.TypeMeta{
				APIVersion: monv1.SchemeGroupVersion.String(),
				Kind:       monv1.ProbesKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
			Spec: monv1.ProbeSpec{},
		},
	}
}

func (b *ProbeBuilder) WithLabel(key string, value string) {
	b.probe.Labels[key] = value
}

func (b *ProbeBuilder) WithAnnotation(key string, value string) {
	b.probe.Annotations[key] = value
}

func (b *ProbeBuilder) WithJobName(jobName string) {
	b.probe.Spec.JobName = jobName
}

func (b *ProbeBuilder) WithModule(module string) {
	b.probe.Spec.Module = module
}

func (b *ProbeBuilder) WithProber(url string, scheme monv1.Scheme, path string) {
	b.probe.Spec.ProberSpec.URL = url
	b.probe.Spec.ProberSpec.Scheme = &scheme
	b.probe.Spec.ProberSpec.Path = path
}

func (b *ProbeBuilder) WithTargets(targets []string) {
	if len(targets) == 0 {
		b.probe.Spec.Targets.StaticConfig = nil
		return
	}

	labels := map[string]string{}
	if b.probe.Spec.Targets.StaticConfig != nil {
		for key, value := range b.probe.Spec.Targets.StaticConfig.Labels {
			labels[key] = value
		}
	}

	b.probe.Spec.Targets.StaticConfig = &monv1.ProbeTargetStaticConfig{
		Targets: targets,
		Labels:  labels,
	}
}

func (b *ProbeBuilder) WithLocation(location infrastructurev1alpha1.Location) {
	b.WithLabel("release", b.config.MonitoringReleaseLabel)
	b.WithLabel("infrastructure.edgecdnx.com/location", location.Name)
	b.WithJobName(fmt.Sprintf("location/%s", location.Name))
	b.WithModule(LocationProbeModule)
	b.WithProber(b.config.LocationProbeProberURL, monv1.Scheme("http"), "")
	b.WithTargets(locationProbeTargets(location, b.config.LocationProbeHealthPath))

	if b.probe.Spec.Targets.StaticConfig != nil {
		b.probe.Spec.Targets.StaticConfig.Labels["location"] = location.Name
		b.probe.Spec.Targets.StaticConfig.Labels["endpoint"] = "location"
	}
}

func (b *ProbeBuilder) Build() (monv1.Probe, string, error) {
	marshalled, err := json.Marshal(b.probe)
	if err != nil {
		return monv1.Probe{}, "", err
	}

	hash := fmt.Sprintf("%x", md5.Sum(marshalled))
	b.WithAnnotation(ValuesHashAnnotation, hash)

	logger := logf.Log.WithName("probe-builder")
	logger.V(1).Info("Built Probe", "name", b.probe.Name, "namespace", b.probe.Namespace, "hash", hash, "marshal", string(marshalled))

	return b.probe, hash, nil
}

func ProbeBuilderFactory(builderType string, name string, namespace string, config ProbeBuilderConfig) (IProbeBuilder, error) {
	switch builderType {
	case "Location":
		return NewDefaultProbeBuilder(name, namespace, config), nil
	default:
		return nil, fmt.Errorf("unknown builder type: %s", builderType)
	}
}

func locationProbeTargets(location infrastructurev1alpha1.Location, healthPath string) []string {
	seen := make(map[string]struct{})

	addTarget := func(address string) {
		if address == "" {
			return
		}

		seen[locationProbeTargetURL(address, healthPath)] = struct{}{}
	}

	for _, node := range location.Spec.Nodes {
		addTarget(node.Ipv4)
		addTarget(node.Ipv6)
	}

	for _, nodeGroup := range location.Spec.NodeGroups {
		for _, node := range nodeGroup.Nodes {
			addTarget(node.Ipv4)
			addTarget(node.Ipv6)
		}
	}

	targets := make([]string, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}

	sort.Strings(targets)
	return targets
}

func locationProbeTargetURL(address string, healthPath string) string {
	if strings.Contains(address, ":") {
		return fmt.Sprintf("http://[%s]%s", address, healthPath)
	}

	return fmt.Sprintf("http://%s%s", address, healthPath)
}
