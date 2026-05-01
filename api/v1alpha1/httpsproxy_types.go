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

// HTTPSProxySpec defines the desired state of HTTPSProxy.
type HTTPSProxySpec struct {
	// ClientRef references the parent NexusClient.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`
	// LocalIP is the local service IP (default 127.0.0.1).
	// +optional
	LocalIP string `json:"localIP,omitempty"`
	// LocalPort is the local service port.
	// +required
	LocalPort int32 `json:"localPort"`
	// RemotePort is the optional port exposed on the frps side.
	// +optional
	RemotePort int32 `json:"remotePort,omitempty"`
	// CustomDomains lists the virtual host domains.
	// +optional
	CustomDomains []string `json:"customDomains,omitempty"`
	// Subdomain is the subdomain for virtual hosting.
	// +optional
	Subdomain string `json:"subdomain,omitempty"`
	// ExtraConfig is raw TOML appended verbatim to this proxy block.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Client",type="string",JSONPath=".spec.clientRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// HTTPSProxy is the Schema for the httpsproxies API.
type HTTPSProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              HTTPSProxySpec `json:"spec,omitempty"`
	Status            ProxyStatus    `json:"status,omitempty"`
}

func (p *HTTPSProxy) GetProxyType() string         { return "https" }
func (p *HTTPSProxy) GetClientRef() LocalObjectRef { return p.Spec.ClientRef }
func (p *HTTPSProxy) GetLocalIP() string           { return p.Spec.LocalIP }
func (p *HTTPSProxy) GetLocalPort() int32          { return p.Spec.LocalPort }
func (p *HTTPSProxy) GetRemotePort() int32         { return p.Spec.RemotePort }
func (p *HTTPSProxy) GetCustomDomains() []string   { return p.Spec.CustomDomains }
func (p *HTTPSProxy) GetSubdomain() string         { return p.Spec.Subdomain }
func (p *HTTPSProxy) GetSecretKey() string         { return "" }
func (p *HTTPSProxy) GetAllowUsers() []string      { return nil }
func (p *HTTPSProxy) GetMultiplexer() string       { return "" }
func (p *HTTPSProxy) GetExtraConfig() string       { return p.Spec.ExtraConfig }
func (p *HTTPSProxy) GetProxyStatus() *ProxyStatus { return &p.Status }

// +kubebuilder:object:root=true

// HTTPSProxyList contains a list of HTTPSProxy.
type HTTPSProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HTTPSProxy `json:"items"`
}

func init() { SchemeBuilder.Register(&HTTPSProxy{}, &HTTPSProxyList{}) }
