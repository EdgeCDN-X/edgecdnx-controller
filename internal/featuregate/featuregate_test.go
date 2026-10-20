package featuregate

import "testing"

func TestEnabled(t *testing.T) {
	tests := []struct {
		name         string
		featureGates string
		gate         string
		want         bool
	}{
		{name: "enabled", featureGates: "cachingreconciler", gate: "cachingreconciler", want: true},
		{name: "comma separated", featureGates: "other, cachingreconciler", gate: "cachingreconciler", want: true},
		{name: "disabled", featureGates: "other", gate: "cachingreconciler", want: false},
		{name: "empty", featureGates: "", gate: "cachingreconciler", want: false},
		{name: "exact match", featureGates: "not-cachingreconciler", gate: "cachingreconciler", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Enabled(tt.featureGates, tt.gate); got != tt.want {
				t.Errorf("Enabled(%q, %q) = %t, want %t", tt.featureGates, tt.gate, got, tt.want)
			}
		})
	}
}
