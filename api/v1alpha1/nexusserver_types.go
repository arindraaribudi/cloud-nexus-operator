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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NexusServerSpec defines the desired state of NexusServer.
type NexusServerSpec struct {
	// Image specifies the container image for frps.
	// +optional
	Image ImageSpec `json:"image,omitempty"`

	// +kubebuilder:default:=1
	Replicas int32 `json:"replicas,omitempty"`

	// +kubebuilder:default:=7000
	BindPort int32 `json:"bindPort,omitempty"`

	// VhostHTTPPort is the port for HTTP virtual host. Optional.
	// +optional
	VhostHTTPPort int32 `json:"vhostHTTPPort,omitempty"`

	// VhostHTTPSPort is the port for HTTPS virtual host. Optional.
	// +optional
	VhostHTTPSPort int32 `json:"vhostHTTPSPort,omitempty"`

	// DashboardPort is the frps dashboard port. Optional.
	// +optional
	DashboardPort int32 `json:"dashboardPort,omitempty"`

	// DashboardUser is the username for the frps dashboard. Optional.
	// +optional
	DashboardUser string `json:"dashboardUser,omitempty"`

	// DashboardPasswordSecret is the secret containing the dashboard password. Optional.
	// +optional
	DashboardPasswordSecret *SecretKeyRef `json:"dashboardPasswordSecret,omitempty"`

	// AuthTokenSecret is the secret containing the auth token frpc must present.
	// When nil, the operator creates a random token secret automatically.
	// +optional
	AuthTokenSecret *SecretKeyRef `json:"authTokenSecret,omitempty"`

	// ExtraConfig is raw TOML appended verbatim to frps.toml.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`

	// Service describes the Kubernetes Service that exposes frps.
	// +optional
	Service ServiceSpec `json:"service,omitempty"`

	// EnableOperatorPlugin enables the frps httpPlugin that notifies the
	// operator when proxies connect/disconnect. Required for RegistryProxy.
	// Default: false
	// +optional
	EnableOperatorPlugin bool `json:"enableOperatorPlugin,omitempty"`

	// OperatorWebhookAddr is the address of the operator frps-plugin HTTP server.
	// Only used when EnableOperatorPlugin is true.
	// Example: "cloud-nexus-operator-controller-manager-frps-plugin.cloud-nexus-operator-system.svc.cluster.local:9090"
	// If empty, defaults to "controller-manager-frps-plugin.<namespace>.svc.cluster.local:9090"
	// where <namespace> is the NexusServer's namespace.
	// +optional
	OperatorWebhookAddr string `json:"operatorWebhookAddr,omitempty"`
}

// NexusServerStatus defines the observed state of NexusServer.
type NexusServerStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Address is the external IP or hostname of the frps Service.
	// +optional
	Address string `json:"address,omitempty"`

	// Port is the bind port of frps.
	// +optional
	Port int32 `json:"port,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Address",type="string",JSONPath=".status.address"
// +kubebuilder:printcolumn:name="Port",type="integer",JSONPath=".status.port"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NexusServer is the Schema for the nexusservers API.
type NexusServer struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec NexusServerSpec `json:"spec"`

	// +optional
	Status NexusServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NexusServerList contains a list of NexusServer.
type NexusServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NexusServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NexusServer{}, &NexusServerList{})
}
