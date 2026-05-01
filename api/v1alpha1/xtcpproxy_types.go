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

// XTCPProxySpec defines the desired state of XTCPProxy.
type XTCPProxySpec struct {
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

// XTCPProxy is the Schema for the xtcpproxies API.
type XTCPProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              XTCPProxySpec `json:"spec,omitempty"`
	Status            ProxyStatus   `json:"status,omitempty"`
}

func (p *XTCPProxy) GetProxyType() string         { return "xtcp" }
func (p *XTCPProxy) GetClientRef() LocalObjectRef { return p.Spec.ClientRef }
func (p *XTCPProxy) GetLocalIP() string           { return p.Spec.LocalIP }
func (p *XTCPProxy) GetLocalPort() int32          { return p.Spec.LocalPort }
func (p *XTCPProxy) GetRemotePort() int32         { return 0 }
func (p *XTCPProxy) GetCustomDomains() []string   { return nil }
func (p *XTCPProxy) GetSubdomain() string         { return "" }
func (p *XTCPProxy) GetSecretKey() string         { return p.Spec.SecretKey }
func (p *XTCPProxy) GetAllowUsers() []string      { return p.Spec.AllowUsers }
func (p *XTCPProxy) GetMultiplexer() string       { return "" }
func (p *XTCPProxy) GetExtraConfig() string       { return p.Spec.ExtraConfig }
func (p *XTCPProxy) GetProxyStatus() *ProxyStatus { return &p.Status }

// +kubebuilder:object:root=true

// XTCPProxyList contains a list of XTCPProxy.
type XTCPProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []XTCPProxy `json:"items"`
}

func init() { SchemeBuilder.Register(&XTCPProxy{}, &XTCPProxyList{}) }
