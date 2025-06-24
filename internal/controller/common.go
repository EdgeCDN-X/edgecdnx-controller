package controller

const HealthStatusHealthy = "Healthy"
const HealthStatusProgressing = "Progressing"
const ValuesHashAnnotation = "edgedcnx.com/values-hash"

type ThrowerOptions struct {
	ThrowerChartName                    string
	ThrowerChartVersion                 string
	ThrowerChartRepository              string
	InfrastructureTargetNamespace       string
	InfrastructureApplicationSetProject string
}
