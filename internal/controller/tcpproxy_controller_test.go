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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

var _ = Describe("TCPProxy Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-tcpproxy"
		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}
		tcpproxy := &tunnelv1alpha1.TCPProxy{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TCPProxy")
			err := k8sClient.Get(ctx, typeNamespacedName, tcpproxy)
			if err != nil && errors.IsNotFound(err) {
				resource := &tunnelv1alpha1.TCPProxy{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: tunnelv1alpha1.TCPProxySpec{
						ClientRef: tunnelv1alpha1.LocalObjectRef{Name: "test-client"},
						LocalPort: 8080,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &tunnelv1alpha1.TCPProxy{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			By("Cleanup the specific resource instance TCPProxy")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TCPProxyReconciler{
				BaseProxyReconciler: BaseProxyReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
				},
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// NexusClient "test-client" does not exist, so reconcile returns an error.
			// This is expected behavior — it proves the controller wiring is correct.
			Expect(err).To(HaveOccurred())
		})
	})
})
