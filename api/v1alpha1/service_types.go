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

type StaticOriginSpec struct {
	// Specifies the Upsteram URL for the static origin
	Upstream string `json:"upstream,omitempty"`
	// Specifies the port for the static origin
	Port int `json:"port,omitempty"`
	// Specifies the Host Header to be used for the static origin
	HostHeader string `json:"hostHeader,omitempty"`
	// Specifies the Scheme for the static origin
	// +kubebuilder:validation:Enum=Http;Https
	Scheme string `json:"scheme,omitempty"`
}

type S3OriginSpec struct {
	// Specifies the S3 Bucket Signature version for the origin
	// +kubebuilder:validation:Enum=2;4
	AwsSigsVersion int `json:"awsSigsVersion"`
	// Specifies the Access Key ID for the private s3 origin
	S3AccessKeyId string `json:"s3AccessKeyId"`
	// Specifies the Secret Access Key for the private s3 origin
	S3SecretKey string `json:"s3SecretKey"`

	// Specifies the S3 Bucket Name for the private s3 origin
	S3BucketName string `json:"s3BucketName"`
	// Specifies the S3 Region for the private s3 origin
	S3Region string `json:"s3Region"`

	// Specifies the S3 Server for the private s3 origin
	S3Server string `json:"s3Server,omitempty"`
	// Specifies the S3 Server Protocol for the private s3 origin
	S3ServerProto string `json:"s3ServerProto,omitempty"`
	// Specifies the S3 Server Port for the private s3 origin
	S3ServerPort int `json:"s3ServerPort,omitempty"`
	// Specifies the S3 Server Style for the private s3 origin
	// +kubebuilder:validation:Enum=virtual;path
	S3Style string `json:"s3Style,omitempty"`
}

type CustomerSpec struct {
	// Specifies the customer name. Only informational
	Name string `json:"name,omitempty"`
	// Specifies the customer Id. Only informational
	Id int `json:"id,omitempty"`
}

type CertificateSpec struct {
	// Specifies the Certificate Object Reference
	CertificateRef string `json:"certificateRef,omitempty"`
	// Specifies the Secret Object Reference for the certificate
	SecretRef string `json:"secretRef,omitempty"`
	// Specifies the Certificate as an inline object
	Crt string `json:"crt,omitempty"`
	// Specifies the Key as an inline object
	Key string `json:"key,omitempty"`
}

type SecureKeySpec struct {
	// Specifies the name of the Secure Key for URL Signatures
	Name string `json:"name,omitempty"`
	// Specifies the value of the Secure Key for URL Signatures
	// +kubebuilder:validation:MinLength=32
	// +kubebuilder:validation:MaxLength=32
	Value string `json:"value,omitempty"`
	// Specifies the creation time of the Secure Key
	CreatedAt metav1.Time `json:"createdAt"`
}

type CacheKeySpec struct {
	// Specifies the list of Query parameters which alter the caching key. If empty, query parameters are not used in the cache key
	// +listType=set
	QueryParams []string `json:"queryParams,omitempty"`
	// Specifies the list of Headers which alter the caching key. If empty, headers are not used in the cache key
	// +listType=set
	Headers []string `json:"headers,omitempty"`
}

// ServiceSpec defines the desired state of Service.
type ServiceSpec struct {
	// Service Name. Use full domain name for the service
	Name string `json:"name,omitempty"`
	// Domain name. Ideally the same as the service name
	Domain string `json:"domain,omitempty"`
	// SSL Certificate for the service
	Certificate CertificateSpec `json:"certificate"`
	// Defines the Origin type for the service. s3 or static
	// +kubebuilder:validation:Enum=s3;static
	OriginType string `json:"originType,omitempty"`
	// Defines the specs of the origin if a static origin is used
	StaticOrigins []StaticOriginSpec `json:"staticOrigins,omitempty"`
	// Defines the specs of the origin if an S3 origin is used
	S3OriginSpec []S3OriginSpec `json:"s3OriginSpec,omitempty"`
	// Defines the secure keys for the service. Max 2 items for key rotation
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=2
	SecureKeys []SecureKeySpec `json:"secureKeys,omitempty"`
	// Defines the customer details for the service
	Customer CustomerSpec `json:"customer"`
	// Specifies which cache to use for the service
	Cache string `json:"cache,omitempty"`
	// Specifies the cache key modifiers for the service
	CacheKeySpec CacheKeySpec `json:"cacheKey"`
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
