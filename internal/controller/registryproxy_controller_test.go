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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

var _ = Describe("RegistryProxy Controller", func() {
	const (
		rpName     = "reg-1"
		clientName = "spoke-client"
		serverName = "main-hub"
		ns         = "default"
	)
	ctx := context.Background()

	nsName := func(name string) types.NamespacedName { return types.NamespacedName{Namespace: ns, Name: name} }
	reconciler := func() *RegistryProxyReconciler {
		return &RegistryProxyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	}
	reconcile_ := func() {
		By("Reconciling the RegistryProxy resource")
		result, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(rpName)})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reconcile error: %v", err))
		By(fmt.Sprintf("Reconcile result: %+v", result))
	}

	BeforeEach(func() {
		// Create prerequisite NexusServer.
		srv := &tunnelv1alpha1.NexusServer{
			ObjectMeta: metav1.ObjectMeta{Name: serverName, Namespace: ns},
			Spec:       tunnelv1alpha1.NexusServerSpec{BindPort: 7000},
		}
		if err := k8sClient.Get(ctx, nsName(serverName), srv); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, srv)).To(Succeed())
		}

		// Create prerequisite NexusClient.
		spoke := &tunnelv1alpha1.NexusClient{
			ObjectMeta: metav1.ObjectMeta{Name: clientName, Namespace: ns},
			Spec: tunnelv1alpha1.NexusClientSpec{
				ServerRef: tunnelv1alpha1.ServerRef{Name: serverName},
				Image:     tunnelv1alpha1.ImageSpec{Repository: "registry.example.com/crd-tunnel", Tag: "1.0.0"},
			},
		}
		if err := k8sClient.Get(ctx, nsName(clientName), spoke); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, spoke)).To(Succeed())
		}

		// Create RegistryProxy.
		rp := &tunnelv1alpha1.RegistryProxy{
			ObjectMeta: metav1.ObjectMeta{Name: rpName, Namespace: ns},
			Spec: tunnelv1alpha1.RegistryProxySpec{
				ClientRef:          tunnelv1alpha1.LocalObjectRef{Name: clientName},
				RemotePort:         15000,
				ServiceAccountName: "registry-proxy-sa",
			},
		}
		if err := k8sClient.Get(ctx, nsName(rpName), rp); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, rp)).To(Succeed())
		}
	})

	AfterEach(func() {
		// Let the test framework handle cleanup via namespace deletion
	})

	It("adds finalizer on first reconcile", func() {
		reconcile_()
		var rp tunnelv1alpha1.RegistryProxy
		Expect(k8sClient.Get(ctx, nsName(rpName), &rp)).To(Succeed())
		Expect(rp.Finalizers).To(ContainElement(tunnelv1alpha1.RegistryCleanupFinalizer))
	})

	It("creates proxy Deployment with correct image and service account", func() {
		reconcile_()
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy"), &dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal("registry-proxy-sa"))
		Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("registry.example.com/crd-tunnel:1.0.0"))
		Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("/registry-proxy"))
	})

	It("creates proxy Service on default port 5000", func() {
		reconcile_()
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy-svc"), &svc)).To(Succeed())
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(5000)))
	})

	It("reconciles successfully with all resources", func() {
		By("Reconciling the RegistryProxy")
		reconcile_()

		By("Verifying RegistryProxy has finalizer")
		var rp tunnelv1alpha1.RegistryProxy
		Expect(k8sClient.Get(ctx, nsName(rpName), &rp)).To(Succeed())
		Expect(rp.Finalizers).To(ContainElement(tunnelv1alpha1.RegistryCleanupFinalizer))

		By("Verifying Deployment was created")
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy"), &dep)).To(Succeed())

		By("Verifying Service was created")
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy-svc"), &svc)).To(Succeed())
	})

	It("removes owned HTTPProxy on deletion", func() {
		reconcile_() // create resources

		var rp tunnelv1alpha1.RegistryProxy
		Expect(k8sClient.Get(ctx, nsName(rpName), &rp)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &rp)).To(Succeed())

		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(rpName)})
		Expect(err).NotTo(HaveOccurred())

		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(rpName+"-registry-proxy"), &hp)).To(
			Satisfy(errors.IsNotFound))
	})

	It("requeues when NexusClient not found", func() {
		// Delete NexusClient first.
		var spoke tunnelv1alpha1.NexusClient
		Expect(k8sClient.Get(ctx, nsName(clientName), &spoke)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &spoke)).To(Succeed())

		result, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(rpName)})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(15 * time.Second))
	})

	It("creates owned HTTPProxy with correct customDomains and location", func() {
		reconcile_()
		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(rpName+"-registry-proxy"), &hp)).To(Succeed())
		Expect(hp.Spec.ClientRef.Name).To(Equal(clientName))
		Expect(hp.Spec.CustomDomains).To(ContainElement(
			fmt.Sprintf("%s-gateway.%s.svc.cluster.local", serverName, ns),
		))
		Expect(hp.Spec.ExtraConfig).To(ContainSubstring("/" + clientName + "/registry"))
	})

	It("creates Deployment with STRIP_PREFIX env var", func() {
		reconcile_()
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy"), &dep)).To(Succeed())
		envMap := map[string]string{}
		for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
			envMap[e.Name] = e.Value
		}
		Expect(envMap["STRIP_PREFIX"]).To(Equal("/" + clientName + "/registry"))
	})
})
