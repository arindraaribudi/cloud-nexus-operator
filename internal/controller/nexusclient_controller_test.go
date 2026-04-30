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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

var _ = Describe("NexusClient Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const serverName = "test-server"
		const ns = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: ns,
		}
		frpclient := &tunnelv1alpha1.NexusClient{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind NexusClient")
			err := k8sClient.Get(ctx, typeNamespacedName, frpclient)
			if err != nil && errors.IsNotFound(err) {
				resource := &tunnelv1alpha1.NexusClient{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: ns,
					},
					Spec: tunnelv1alpha1.NexusClientSpec{
						ServerRef: tunnelv1alpha1.ServerRef{
							Address: "127.0.0.1",
							Port:    7000,
						},
						Image: tunnelv1alpha1.ImageSpec{
							Repository: "registry.example.com/crd-tunnel",
							Tag:        "1.0.0",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// Cleanup resources
			_ = k8sClient.Delete(ctx, &tunnelv1alpha1.NexusClient{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: ns}})
		})

		It("should successfully create the resource", func() {
			By("Verifying the NexusClient resource was created")
			var nc tunnelv1alpha1.NexusClient
			Expect(k8sClient.Get(ctx, typeNamespacedName, &nc)).To(Succeed())
			Expect(nc.Spec.ServerRef.Address).To(Equal("127.0.0.1"))
			Expect(nc.Spec.ServerRef.Port).To(Equal(int32(7000)))
		})
	})
})
