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

// TCPMuxProxySpec defines the desired state of TCPMuxProxy.
type TCPMuxProxySpec struct {
	// ClientRef references the parent NexusClient.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`
	// LocalIP is the local service IP (default 127.0.0.1).
	// +optional
	LocalIP string `json:"localIP,omitempty"`
	// LocalPort is the local service port.
	// +required
	LocalPort int32 `json:"localPort"`
	// CustomDomains lists the virtual host domains.
	// +optional
	CustomDomains []string `json:"customDomains,omitempty"`
	// Subdomain is the subdomain for virtual hosting.
	// +optional
	Subdomain string `json:"subdomain,omitempty"`
	// Multiplexer is the multiplexer type. Default: httpconnect.
	// +kubebuilder:default:="httpconnect"
	// +optional
	Multiplexer string `json:"multiplexer,omitempty"`
	// ExtraConfig is raw TOML appended verbatim to this proxy block.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Client",type="string",JSONPath=".spec.clientRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TCPMuxProxy is the Schema for the tcpmuxproxies API.
type TCPMuxProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TCPMuxProxySpec `json:"spec,omitempty"`
	Status            ProxyStatus     `json:"status,omitempty"`
}

func (p *TCPMuxProxy) GetProxyType() string         { return "tcpmux" }
func (p *TCPMuxProxy) GetClientRef() LocalObjectRef { return p.Spec.ClientRef }
func (p *TCPMuxProxy) GetLocalIP() string           { return p.Spec.LocalIP }
func (p *TCPMuxProxy) GetLocalPort() int32          { return p.Spec.LocalPort }
func (p *TCPMuxProxy) GetRemotePort() int32         { return 0 }
func (p *TCPMuxProxy) GetCustomDomains() []string   { return p.Spec.CustomDomains }
func (p *TCPMuxProxy) GetSubdomain() string         { return p.Spec.Subdomain }
func (p *TCPMuxProxy) GetSecretKey() string         { return "" }
func (p *TCPMuxProxy) GetAllowUsers() []string      { return nil }
func (p *TCPMuxProxy) GetMultiplexer() string       { return p.Spec.Multiplexer }
func (p *TCPMuxProxy) GetExtraConfig() string       { return p.Spec.ExtraConfig }
func (p *TCPMuxProxy) GetProxyStatus() *ProxyStatus { return &p.Status }

// +kubebuilder:object:root=true

// TCPMuxProxyList contains a list of TCPMuxProxy.
type TCPMuxProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TCPMuxProxy `json:"items"`
}

func init() { SchemeBuilder.Register(&TCPMuxProxy{}, &TCPMuxProxyList{}) }
