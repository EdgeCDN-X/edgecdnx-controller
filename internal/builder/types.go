package builder

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
