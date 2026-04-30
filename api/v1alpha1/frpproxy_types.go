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

const (
	// ProxyCleanupFinalizer is the finalizer added to FrpProxy resources.
	// Deletion triggers a config reload before the resource is removed.
	ProxyCleanupFinalizer = "tunnel.io/proxy-cleanup"
)

// FrpProxySpec defines the desired state of FrpProxy.
type FrpProxySpec struct {
	// ClientRef references the parent NexusClient.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`

	// Type is the tunnel type (tcp, udp, http, https, stcp, xtcp, tcpmux).
	// +kubebuilder:validation:Enum=tcp;udp;http;https;stcp;xtcp;tcpmux
	// +required
	Type string `json:"type"`

	// LocalIP is the local service IP (default 127.0.0.1).
	// +optional
	LocalIP string `json:"localIP,omitempty"`

	// LocalPort is the local service port.
	// +required
	LocalPort int32 `json:"localPort"`

	// RemotePort is the port exposed on the frps side (tcp/udp).
	// +optional
	RemotePort int32 `json:"remotePort,omitempty"`

	// CustomDomains lists the virtual host domains (http/https).
	// +optional
	CustomDomains []string `json:"customDomains,omitempty"`

	// SecretKey is the shared secret for stcp/xtcp.
	// +optional
	SecretKey string `json:"secretKey,omitempty"`

	// ExtraConfig is raw TOML appended verbatim to this proxy block.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// LocalObjectRef is a reference to an object in the same namespace.
type LocalObjectRef struct {
	// Name is the name of the referenced object.
	Name string `json:"name"`
}

// FrpProxyStatus defines the observed state of FrpProxy.
type FrpProxyStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// LastReloadTime is the time of the last successful hot reload.
	// +optional
	LastReloadTime *metav1.Time `json:"lastReloadTime,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Client",type="string",JSONPath=".spec.clientRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// FrpProxy is the Schema for the frpproxies API.
type FrpProxy struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec FrpProxySpec `json:"spec"`

	// +optional
	Status FrpProxyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// FrpProxyList contains a list of FrpProxy.
type FrpProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []FrpProxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrpProxy{}, &FrpProxyList{})
}
