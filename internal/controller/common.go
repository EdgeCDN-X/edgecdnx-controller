package controller

const HealthStatusHealthy = "Healthy"
const ValuesHashAnnotation = "edgedcnx.com/values-hash"

type ThrowerOptions struct {
	ThrowerChartName                      string
	ThrowerChartVersion                   string
	ThrowerChartRepository                string
	InfrastructureApplicationSetNamespace string
	InfrastructureTargetNamespace         string
	InfrastructureApplicationSetProject   string
}
