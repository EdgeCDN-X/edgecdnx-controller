/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HealthCheckProbeType defines how a healthcheck probe determines health.
// +kubebuilder:validation:Enum=TCP;HTTP;HTTPS;ASSUME
type HealthCheckProbeType string

const (
	HealthCheckProbeTypeTCP    HealthCheckProbeType = "TCP"
	HealthCheckProbeTypeHTTP   HealthCheckProbeType = "HTTP"
	HealthCheckProbeTypeHTTPS  HealthCheckProbeType = "HTTPS"
	HealthCheckProbeTypeASSUME HealthCheckProbeType = "ASSUME"
)

// AssumedHealthStatus defines the static health value returned by an ASSUME probe.
// +kubebuilder:validation:Enum=Healthy;Unhealthy
type AssumedHealthStatus string

const (
	AssumedHealthStatusHealthy   AssumedHealthStatus = "Healthy"
	AssumedHealthStatusUnhealthy AssumedHealthStatus = "Unhealthy"
)

// TCPHealthCheckProbeSpec defines settings for TCP healthchecks.
type TCPHealthCheckProbeSpec struct {
	// Port is the TCP port to connect to.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// HTTPHealthCheckProbeSpec defines settings for HTTP and HTTPS healthchecks.
type HTTPHealthCheckProbeSpec struct {
	// Port is the HTTP or HTTPS port to connect to.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
	// Path is the request path used for the healthcheck.
	// +kubebuilder:default="/"
	Path string `json:"path,omitempty"`
	// Host is the optional Host header used for the healthcheck request.
	Host string `json:"host,omitempty"`
}

// AssumeHealthCheckProbeSpec defines settings for static healthchecks.
type AssumeHealthCheckProbeSpec struct {
	// Status is the health value returned without performing an active healthcheck.
	Status AssumedHealthStatus `json:"status"`
}

// HealthCheckProbeSpec defines one probe in a healthcheck profile.
// +kubebuilder:validation:XValidation:rule="self.type == 'TCP' ? has(self.tcp) : !has(self.tcp)",message="tcp must be set only when type is TCP"
// +kubebuilder:validation:XValidation:rule="self.type == 'HTTP' ? has(self.http) : !has(self.http)",message="http must be set only when type is HTTP"
// +kubebuilder:validation:XValidation:rule="self.type == 'HTTPS' ? has(self.https) : !has(self.https)",message="https must be set only when type is HTTPS"
// +kubebuilder:validation:XValidation:rule="self.type == 'ASSUME' ? has(self.assume) : !has(self.assume)",message="assume must be set only when type is ASSUME"
type HealthCheckProbeSpec struct {
	// Name is the unique probe name within this profile.
	Name string `json:"name"`
	// Type determines which probe configuration is used.
	Type HealthCheckProbeType `json:"type"`
	// Interval is how often the probe runs.
	Interval metav1.Duration `json:"interval,omitempty"`
	// Timeout is how long to wait for a probe result before treating it as failed.
	Timeout metav1.Duration `json:"timeout,omitempty"`
	// TCP configures TCP probes. Used when type is TCP.
	TCP *TCPHealthCheckProbeSpec `json:"tcp,omitempty"`
	// HTTP configures HTTP probes. Used when type is HTTP.
	HTTP *HTTPHealthCheckProbeSpec `json:"http,omitempty"`
	// HTTPS configures HTTPS probes. Used when type is HTTPS.
	HTTPS *HTTPHealthCheckProbeSpec `json:"https,omitempty"`
	// Assume configures static health. Used when type is ASSUME.
	Assume *AssumeHealthCheckProbeSpec `json:"assume,omitempty"`
}

// HealthCheckProfileSpec defines the desired state of HealthCheckProfile.
type HealthCheckProfileSpec struct {
	// Probes defines the healthchecks that can be applied to locations or nodes.
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	Probes []HealthCheckProbeSpec `json:"probes,omitempty"`
}

// HealthCheckProfileStatus defines the observed state of HealthCheckProfile.
type HealthCheckProfileStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:object:generate=true

// HealthCheckProfile is the Schema for the healthcheckprofiles API.
type HealthCheckProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HealthCheckProfileSpec   `json:"spec,omitempty"`
	Status HealthCheckProfileStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true

// HealthCheckProfileList contains a list of HealthCheckProfile.
type HealthCheckProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HealthCheckProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HealthCheckProfile{}, &HealthCheckProfileList{})
}
