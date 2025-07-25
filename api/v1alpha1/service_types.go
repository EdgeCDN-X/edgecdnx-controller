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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type StaticOriginSpec struct {
	Upstream   string `json:"upstream,omitempty"`
	Port       int    `json:"port,omitempty"`
	HostHeader string `json:"hostHeader,omitempty"`
	// +kubebuilder:validation:Enum=Http;Https
	Scheme string `json:"scheme,omitempty"`
}

type S3OriginSpec struct {
	// +kubebuilder:validation:Enum=2;4
	AwsSigsVersion int    `json:"awsSigsVersion"`
	S3AccessKeyId  string `json:"s3AccessKeyId"`
	S3SecretKey    string `json:"s3SecretKey"`

	// AWS Endpoints
	S3BucketName string `json:"s3BucketName"`
	S3Region     string `json:"s3Region"`

	// Custom S3 endpoitns
	S3Server      string `json:"s3Server,omitempty"`
	S3ServerProto string `json:"s3ServerProto,omitempty"`
	S3ServerPort  int    `json:"s3ServerPort,omitempty"`
	// +kubebuilder:validation:Enum=virtual;path
	S3Style string `json:"s3Style,omitempty"`
}

type CustomerSpec struct {
	Name string `json:"name,omitempty"`
	Id   int    `json:"id,omitempty"`
}

type CertificateSpec struct {
	CertificateRef string `json:"certificateRef,omitempty"`
	SecretRef      string `json:"secretRef,omitempty"`
	Crt            string `json:"crt,omitempty"`
	Key            string `json:"key,omitempty"`
}

type SecureKeySpec struct {
	Name      string      `json:"name,omitempty"`
	Value     string      `json:"value,omitempty"`
	CreatedAt metav1.Time `json:"createdAt"`
}

type CacheKeySpec struct {
	QueryParams []string `json:"queryParams,omitempty"`
	Headers     []string `json:"headers,omitempty"`
}

// ServiceSpec defines the desired state of Service.
type ServiceSpec struct {
	Name        string          `json:"name,omitempty"`
	Domain      string          `json:"domain,omitempty"`
	Certificate CertificateSpec `json:"certificate"`
	// +kubebuilder:validation:Enum=s3;static
	OriginType    string             `json:"originType,omitempty"`
	StaticOrigins []StaticOriginSpec `json:"staticOrigins,omitempty"`
	S3OriginSpec  []S3OriginSpec     `json:"s3OriginSpec,omitempty"`
	SecureKeys    []SecureKeySpec    `json:"secureKeys,omitempty"`
	Customer      CustomerSpec       `json:"customer"`
	Cache         string             `json:"cache,omitempty"`
	CacheKeySpec  CacheKeySpec       `json:"cacheKey"`
}

// ServiceStatus defines the observed state of Service.
type ServiceStatus struct {
	// +kubebuilder:validation:Enum=Healthy;Progressing;Degraded
	Status string `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Service is the Schema for the services API.
type Service struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceSpec   `json:"spec,omitempty"`
	Status ServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceList contains a list of Service.
type ServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Service `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Service{}, &ServiceList{})
}
