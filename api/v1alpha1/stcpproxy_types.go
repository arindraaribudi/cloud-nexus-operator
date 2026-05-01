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

// STCPProxySpec defines the desired state of STCPProxy.
type STCPProxySpec struct {
	// ClientRef references the parent NexusClient.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`
	// LocalIP is the local service IP (default 127.0.0.1).
	// +optional
	LocalIP string `json:"localIP,omitempty"`
	// LocalPort is the local service port.
	// +required
	LocalPort int32 `json:"localPort"`
	// SecretKey is the shared secret that visitors must present.
	// +optional
	SecretKey string `json:"secretKey,omitempty"`
	// AllowUsers lists the visitor names allowed to connect.
	// +optional
	AllowUsers []string `json:"allowUsers,omitempty"`
	// ExtraConfig is raw TOML appended verbatim to this proxy block.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Client",type="string",JSONPath=".spec.clientRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// STCPProxy is the Schema for the stcpproxies API.
type STCPProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              STCPProxySpec `json:"spec,omitempty"`
	Status            ProxyStatus   `json:"status,omitempty"`
}

func (p *STCPProxy) GetProxyType() string         { return "stcp" }
func (p *STCPProxy) GetClientRef() LocalObjectRef { return p.Spec.ClientRef }
func (p *STCPProxy) GetLocalIP() string           { return p.Spec.LocalIP }
func (p *STCPProxy) GetLocalPort() int32          { return p.Spec.LocalPort }
func (p *STCPProxy) GetRemotePort() int32         { return 0 }
func (p *STCPProxy) GetCustomDomains() []string   { return nil }
func (p *STCPProxy) GetSubdomain() string         { return "" }
func (p *STCPProxy) GetSecretKey() string         { return p.Spec.SecretKey }
func (p *STCPProxy) GetAllowUsers() []string      { return p.Spec.AllowUsers }
func (p *STCPProxy) GetMultiplexer() string       { return "" }
func (p *STCPProxy) GetExtraConfig() string       { return p.Spec.ExtraConfig }
func (p *STCPProxy) GetProxyStatus() *ProxyStatus { return &p.Status }

// +kubebuilder:object:root=true

// STCPProxyList contains a list of STCPProxy.
type STCPProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []STCPProxy `json:"items"`
}

func init() { SchemeBuilder.Register(&STCPProxy{}, &STCPProxyList{}) }
