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

// TCPProxySpec defines the desired state of TCPProxy.
type TCPProxySpec struct {
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

// TCPProxy is the Schema for the tcpproxies API.
type TCPProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TCPProxySpec `json:"spec,omitempty"`
	Status            ProxyStatus  `json:"status,omitempty"`
}

func (p *TCPProxy) GetProxyType() string         { return "tcp" }
func (p *TCPProxy) GetClientRef() LocalObjectRef { return p.Spec.ClientRef }
func (p *TCPProxy) GetLocalIP() string           { return p.Spec.LocalIP }
func (p *TCPProxy) GetLocalPort() int32          { return p.Spec.LocalPort }
func (p *TCPProxy) GetRemotePort() int32         { return p.Spec.RemotePort }
func (p *TCPProxy) GetCustomDomains() []string   { return nil }
func (p *TCPProxy) GetSubdomain() string         { return "" }
func (p *TCPProxy) GetSecretKey() string         { return "" }
func (p *TCPProxy) GetAllowUsers() []string      { return nil }
func (p *TCPProxy) GetMultiplexer() string       { return "" }
func (p *TCPProxy) GetExtraConfig() string       { return p.Spec.ExtraConfig }
func (p *TCPProxy) GetProxyStatus() *ProxyStatus { return &p.Status }

// +kubebuilder:object:root=true

// TCPProxyList contains a list of TCPProxy.
type TCPProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TCPProxy `json:"items"`
}

func init() { SchemeBuilder.Register(&TCPProxy{}, &TCPProxyList{}) }
