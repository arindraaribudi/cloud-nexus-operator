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

// ServerRef holds either a name reference to an NexusServer CR or explicit
// address/port coordinates.
type ServerRef struct {
	// Name references an NexusServer in the same namespace; the operator will
	// look up its status.address and status.port.
	// +optional
	Name string `json:"name,omitempty"`

	// Address is an explicit server host or IP (used when Name is empty).
	// +optional
	Address string `json:"address,omitempty"`

	// Port is an explicit server port (used when Name is empty).
	// +optional
	Port int32 `json:"port,omitempty"`

	// DiscoveryPort is the nexus-discovery port on the server.
	// When non-zero and Name is empty, the NexusClient controller calls
	// http://{address}:{discoveryPort}/api/nexus-info to auto-discover
	// the hub gateway FQDN and port, storing them in status.
	// +optional
	DiscoveryPort int32 `json:"discoveryPort,omitempty"`
}

// AdminSpec describes the frpc local admin API configuration.
type AdminSpec struct {
	// +kubebuilder:default:=7400
	Port int32 `json:"port,omitempty"`

	// TokenSecret is the secret containing the admin API bearer token.
	// When nil, the operator creates a random token secret automatically.
	// +optional
	TokenSecret *SecretKeyRef `json:"tokenSecret,omitempty"`
}

// NexusClientSpec defines the desired state of NexusClient.
type NexusClientSpec struct {
	// Image specifies the container image for frpc.
	// +optional
	Image ImageSpec `json:"image,omitempty"`

	// ServerRef resolves the address of the NexusServer.
	// +required
	ServerRef ServerRef `json:"serverRef"`

	// AuthTokenSecret is the secret containing the auth token presented to frps.
	// When nil, the operator inherits or creates a token automatically.
	// +optional
	AuthTokenSecret *SecretKeyRef `json:"authTokenSecret,omitempty"`

	// Admin configures the frpc local admin API (used for hot reload).
	// +optional
	Admin AdminSpec `json:"admin,omitempty"`

	// ExtraConfig is raw TOML appended verbatim to frpc.toml.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// NexusClientStatus defines the observed state of NexusClient.
type NexusClientStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// ServerAddress is the resolved server address (host:port).
	// +optional
	ServerAddress string `json:"serverAddress,omitempty"`

	// GatewayFQDN is the hub-side gateway service FQDN, populated by the
	// NexusClient controller via local NexusServer lookup or discovery endpoint.
	// Read by RegistryProxy and CloudRestApiProxy controllers.
	// +optional
	GatewayFQDN string `json:"gatewayFQDN,omitempty"`

	// GatewayPort is the vhostHTTPPort on the hub gateway.
	// +optional
	GatewayPort int32 `json:"gatewayPort,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Server",type="string",JSONPath=".status.serverAddress"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NexusClient is the Schema for the nexusclients API.
type NexusClient struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec NexusClientSpec `json:"spec"`

	// +optional
	Status NexusClientStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NexusClientList contains a list of NexusClient.
type NexusClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NexusClient `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NexusClient{}, &NexusClientList{})
}
