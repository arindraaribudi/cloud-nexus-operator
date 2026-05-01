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

// UDPProxySpec defines the desired state of UDPProxy.
type UDPProxySpec struct {
	// ClientRef references the parent NexusClient.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`
	// LocalIP is the local service IP (default 127.0.0.1).
	// +optional
	LocalIP string `json:"localIP,omitempty"`
	// LocalPort is the local service port.
	// +required
	LocalPort int32 `json:"localPort"`
	// RemotePort is the port exposed on the frps side.
	// +optional
	RemotePort int32 `json:"remotePort,omitempty"`
	// ExtraConfig is raw TOML appended verbatim to this proxy block.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Client",type="string",JSONPath=".spec.clientRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// UDPProxy is the Schema for the udpproxies API.
type UDPProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              UDPProxySpec `json:"spec,omitempty"`
	Status            ProxyStatus  `json:"status,omitempty"`
}

func (p *UDPProxy) GetProxyType() string         { return "udp" }
func (p *UDPProxy) GetClientRef() LocalObjectRef { return p.Spec.ClientRef }
func (p *UDPProxy) GetLocalIP() string           { return p.Spec.LocalIP }
func (p *UDPProxy) GetLocalPort() int32          { return p.Spec.LocalPort }
func (p *UDPProxy) GetRemotePort() int32         { return p.Spec.RemotePort }
func (p *UDPProxy) GetCustomDomains() []string   { return nil }
func (p *UDPProxy) GetSubdomain() string         { return "" }
func (p *UDPProxy) GetSecretKey() string         { return "" }
func (p *UDPProxy) GetAllowUsers() []string      { return nil }
func (p *UDPProxy) GetMultiplexer() string       { return "" }
func (p *UDPProxy) GetExtraConfig() string       { return p.Spec.ExtraConfig }
func (p *UDPProxy) GetProxyStatus() *ProxyStatus { return &p.Status }

// +kubebuilder:object:root=true

// UDPProxyList contains a list of UDPProxy.
type UDPProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UDPProxy `json:"items"`
}

func init() { SchemeBuilder.Register(&UDPProxy{}, &UDPProxyList{}) }
