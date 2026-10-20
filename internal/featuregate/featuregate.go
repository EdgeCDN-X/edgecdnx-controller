package featuregate

import "strings"

func Enabled(featureGates, name string) bool {
	for _, featureGate := range strings.Split(featureGates, ",") {
		if strings.TrimSpace(featureGate) == name {
			return true
		}
	}

	return false
}
