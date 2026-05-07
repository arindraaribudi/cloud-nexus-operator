/*
Copyright 2026.

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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// CloudRestApiProxyCleanupFinalizer is added to CloudRestApiProxy resources.
	CloudRestApiProxyCleanupFinalizer = "tunnel.io/cloudrestapiproxy-cleanup"
)

// CloudRestApiProxySpec defines the desired state of CloudRestApiProxy.
type CloudRestApiProxySpec struct {
	// ClientRef references the NexusClient (spoke) that owns the tunnel.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`

	// ServiceAccountName is the Kubernetes SA annotated with cloud credentials
	// (IRSA for AWS, Workload Identity for GCP, Azure Workload Identity for Azure).
	// +required
	ServiceAccountName string `json:"serviceAccountName"`

	// ProxyPort is the port the cloud-proxy pod listens on inside the spoke.
	// +optional
	// +kubebuilder:default=8080
	ProxyPort int32 `json:"proxyPort,omitempty"`

	// AllowedDomains is an optional allowlist of upstream cloud FQDNs.
	// Supports wildcards: ["*.amazonaws.com", "*.googleapis.com"].
	// If empty, all domains are permitted.
	// +optional
	AllowedDomains []string `json:"allowedDomains,omitempty"`

	// Image overrides the default cloud-proxy container image.
	// +optional
	Image *ImageSpec `json:"image,omitempty"`
}

// CloudRestApiProxyStatus defines the observed state of CloudRestApiProxy.
type CloudRestApiProxyStatus struct {
	// Phase is Pending, Running, or Failed.
	// +optional
	Phase string `json:"phase,omitempty"`

	// GatewayURL is the base URL on the hub gateway for this proxy.
	// e.g. http://hub-gateway.default.svc.cluster.local:8080/spoke1/cloud
	// +optional
	GatewayURL string `json:"gatewayURL,omitempty"`

	// Conditions reports detailed lifecycle state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="GatewayURL",type="string",JSONPath=".status.gatewayURL"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CloudRestApiProxy is the Schema for the cloudrestapiproxies API.
type CloudRestApiProxy struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec CloudRestApiProxySpec `json:"spec"`

	// +optional
	Status CloudRestApiProxyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CloudRestApiProxyList contains a list of CloudRestApiProxy.
type CloudRestApiProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CloudRestApiProxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudRestApiProxy{}, &CloudRestApiProxyList{})
}
