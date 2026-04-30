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

import corev1 "k8s.io/api/core/v1"

// ImageSpec holds the container image repository and tag.
type ImageSpec struct {
	// +kubebuilder:default:="asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel"
	Repository string `json:"repository,omitempty"`
	// +kubebuilder:default:="1.0.0"
	Tag string `json:"tag,omitempty"`
}

// SecretKeyRef points to a key inside a Kubernetes Secret.
type SecretKeyRef struct {
	// Name is the name of the Secret.
	Name string `json:"name"`
	// Key is the key within the Secret.
	Key string `json:"key"`
}

// ServiceSpec describes the Kubernetes Service that exposes the frps pod.
type ServiceSpec struct {
	// +kubebuilder:default:="LoadBalancer"
	Type corev1.ServiceType `json:"type,omitempty"`
	// Annotations are passed verbatim to the Service metadata.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Ports defines the ports to expose.
	// +optional
	Ports []corev1.ServicePort `json:"ports,omitempty"`
}
