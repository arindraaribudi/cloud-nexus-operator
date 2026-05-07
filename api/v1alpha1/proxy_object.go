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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ProxyStatus is the observed state shared by all typed proxy CRDs.
type ProxyStatus struct {
	// Phase is the current lifecycle phase (Pending/Running/Failed).
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

// +kubebuilder:object:generate=false

// ProxyObject is implemented by all 8 typed proxy CRDs.
// It allows BaseProxyReconciler to operate generically over all proxy types.
type ProxyObject interface {
	client.Object

	// GetProxyType returns the FRP proxy type string (e.g. "tcp", "http").
	GetProxyType() string
	// GetClientRef returns the reference to the parent NexusClient.
	GetClientRef() LocalObjectRef
	// GetLocalIP returns the local service IP. Empty means default 127.0.0.1.
	GetLocalIP() string
	// GetLocalPort returns the local service port.
	GetLocalPort() int32
	// GetRemotePort returns the remote port (tcp/udp/http/https). Zero means unset.
	GetRemotePort() int32
	// GetCustomDomains returns virtual host domains (http/https/tcpmux).
	GetCustomDomains() []string
	// GetSubdomain returns the subdomain (http/https/tcpmux).
	GetSubdomain() string
	// GetSecretKey returns the shared secret (stcp/xtcp/sudp).
	GetSecretKey() string
	// GetAllowUsers returns allowed visitor list (stcp/xtcp/sudp).
	GetAllowUsers() []string
	// GetMultiplexer returns the multiplexer type (tcpmux only, e.g. "httpconnect").
	GetMultiplexer() string
	// GetExtraConfig returns raw TOML appended verbatim.
	GetExtraConfig() string
	// GetProxyStatus returns a pointer to the embedded ProxyStatus.
	GetProxyStatus() *ProxyStatus
}
