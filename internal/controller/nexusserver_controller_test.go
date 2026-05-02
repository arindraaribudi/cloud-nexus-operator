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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

var _ = Describe("NexusServer Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		frpserver := &tunnelv1alpha1.NexusServer{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind NexusServer")
			err := k8sClient.Get(ctx, typeNamespacedName, frpserver)
			if err != nil && errors.IsNotFound(err) {
				resource := &tunnelv1alpha1.NexusServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &tunnelv1alpha1.NexusServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance NexusServer")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &NexusServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

var _ = Describe("NexusServer gateway Service", func() {
	const (
		gwNs = "default"
	)
	ctx := context.Background()
	nsName := func(name string) types.NamespacedName {
		return types.NamespacedName{Namespace: gwNs, Name: name}
	}

	Context("when vhostHTTPPort is set", func() {
		const gwServerName = "hub-with-vhost"

		AfterEach(func() {
			srv := &tunnelv1alpha1.NexusServer{}
			if err := k8sClient.Get(ctx, nsName(gwServerName), srv); err == nil {
				_ = k8sClient.Delete(ctx, srv)
			}
		})

		It("creates gateway Service", func() {
			srv := &tunnelv1alpha1.NexusServer{
				ObjectMeta: metav1.ObjectMeta{Name: gwServerName, Namespace: gwNs},
				Spec: tunnelv1alpha1.NexusServerSpec{
					BindPort:           7000,
					VhostHTTPPort:      8080,
					GatewayServiceType: "ClusterIP",
				},
			}
			Expect(k8sClient.Create(ctx, srv)).To(Succeed())

			r := &NexusServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsName(gwServerName)})
			Expect(err).NotTo(HaveOccurred())

			var gw corev1.Service
			Expect(k8sClient.Get(ctx, nsName(gwServerName+"-gateway"), &gw)).To(Succeed())
			Expect(gw.Spec.Ports[0].Port).To(Equal(int32(8080)))
			Expect(gw.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		})
	})

	Context("when vhostHTTPPort is 0", func() {
		const gwServerName = "hub-no-vhost"

		AfterEach(func() {
			srv := &tunnelv1alpha1.NexusServer{}
			if err := k8sClient.Get(ctx, nsName(gwServerName), srv); err == nil {
				_ = k8sClient.Delete(ctx, srv)
			}
		})

		It("does not create gateway Service", func() {
			srv := &tunnelv1alpha1.NexusServer{
				ObjectMeta: metav1.ObjectMeta{Name: gwServerName, Namespace: gwNs},
				Spec:       tunnelv1alpha1.NexusServerSpec{BindPort: 7000},
			}
			Expect(k8sClient.Create(ctx, srv)).To(Succeed())

			r := &NexusServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsName(gwServerName)})
			Expect(err).NotTo(HaveOccurred())

			var gw corev1.Service
			err = k8sClient.Get(ctx, nsName(gwServerName+"-gateway"), &gw)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
