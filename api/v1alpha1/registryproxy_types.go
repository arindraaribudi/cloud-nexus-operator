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
	// RegistryCleanupFinalizer is added to RegistryProxy resources.
	// Deletion removes owned FrpProxy/Deployment/Service before the CR is removed.
	RegistryCleanupFinalizer = "tunnel.io/registryproxy-cleanup"
)

// RegistryProxySpec defines the desired state of RegistryProxy.
type RegistryProxySpec struct {
	// ClientRef references the NexusClient (spoke) that owns the tunnel.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`

	// RemotePort is the port frps will bind on the hub side.
	// Must not conflict with other FrpProxy remotePort values on the same NexusClient.
	// +required
	RemotePort int32 `json:"remotePort"`

	// ServiceAccountName is the Kubernetes SA annotated with WI/IRSA for cloud auth.
	// +required
	ServiceAccountName string `json:"serviceAccountName"`

	// ProxyPort is the port the registry-proxy binary listens on inside the Pod.
	// Default: 5000
	// +optional
	ProxyPort *int32 `json:"proxyPort,omitempty"`

	// Image overrides the proxy container image.
	// Defaults to the image used by the referenced NexusClient.
	// +optional
	Image *ImageSpec `json:"image,omitempty"`
}

// RegistryProxyStatus defines the observed state of RegistryProxy.
type RegistryProxyStatus struct {
	// Phase is the current lifecycle phase: Pending | Running | Failed.
	// +optional
	Phase string `json:"phase,omitempty"`

	// HubService is the FQDN of the hub-side Service once created.
	// Example: reg-1-docker.tunnels.svc.cluster.local
	// +optional
	HubService string `json:"hubService,omitempty"`

	// Conditions reports the detailed lifecycle state.
	// Known condition types:
	//   ProxyReady      - registry-proxy Deployment is healthy
	//   TunnelConnected - owned FrpProxy phase is Running (tunnel is active)
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="HubService",type="string",JSONPath=".status.hubService"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// RegistryProxy is the Schema for the registryproxies API.
type RegistryProxy struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec RegistryProxySpec `json:"spec"`

	// +optional
	Status RegistryProxyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// RegistryProxyList contains a list of RegistryProxy.
type RegistryProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []RegistryProxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RegistryProxy{}, &RegistryProxyList{})
}
