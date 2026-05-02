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

var _ = Describe("CloudRestApiProxy Controller", func() {
	const (
		crName    = "spoke1-cloud"
		crClient  = "spoke1-client"
		crServer  = "main-hub"
		ns        = "default"
		vhostPort = int32(8080)
	)
	ctx := context.Background()
	nsName := func(name string) types.NamespacedName {
		return types.NamespacedName{Namespace: ns, Name: name}
	}
	reconciler := func() *CloudRestApiProxyReconciler {
		return &CloudRestApiProxyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	}
	reconcile_ := func() {
		By("Reconciling CloudRestApiProxy")
		result, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(crName)})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reconcile error: %v", err))
		By(fmt.Sprintf("result: %+v", result))
	}

	BeforeEach(func() {
		// NexusServer with vhostHTTPPort set.
		srv := &tunnelv1alpha1.NexusServer{
			ObjectMeta: metav1.ObjectMeta{Name: crServer, Namespace: ns},
			Spec:       tunnelv1alpha1.NexusServerSpec{BindPort: 7000, VhostHTTPPort: vhostPort},
		}
		if err := k8sClient.Get(ctx, nsName(crServer), srv); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, srv)).To(Succeed())
		}
		// NexusClient referencing the server.
		spoke := &tunnelv1alpha1.NexusClient{
			ObjectMeta: metav1.ObjectMeta{Name: crClient, Namespace: ns},
			Spec: tunnelv1alpha1.NexusClientSpec{
				ServerRef: tunnelv1alpha1.ServerRef{Name: crServer},
				Image:     tunnelv1alpha1.ImageSpec{Repository: "registry.example.com/crd-tunnel", Tag: "1.0.0"},
			},
		}
		if err := k8sClient.Get(ctx, nsName(crClient), spoke); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, spoke)).To(Succeed())
		}
		// CloudRestApiProxy CR.
		cr := &tunnelv1alpha1.CloudRestApiProxy{
			ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: ns},
			Spec: tunnelv1alpha1.CloudRestApiProxySpec{
				ClientRef:          tunnelv1alpha1.LocalObjectRef{Name: crClient},
				ServiceAccountName: "cloud-proxy-sa",
				AllowedDomains:     []string{"*.amazonaws.com"},
			},
		}
		if err := k8sClient.Get(ctx, nsName(crName), cr); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		}
	})

	It("adds finalizer on first reconcile", func() {
		reconcile_()
		var cr tunnelv1alpha1.CloudRestApiProxy
		Expect(k8sClient.Get(ctx, nsName(crName), &cr)).To(Succeed())
		Expect(cr.Finalizers).To(ContainElement(tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer))
	})

	It("creates cloud-proxy Deployment with correct env vars", func() {
		reconcile_()
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nsName(crName+"-cloud-proxy"), &dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal("cloud-proxy-sa"))
		envVars := dep.Spec.Template.Spec.Containers[0].Env
		envMap := map[string]string{}
		for _, e := range envVars {
			envMap[e.Name] = e.Value
		}
		Expect(envMap["STRIP_PREFIX"]).To(Equal("/" + crClient + "/cloud"))
		Expect(envMap["ALLOWED_DOMAINS"]).To(Equal("*.amazonaws.com"))
	})

	It("creates spoke-side ClusterIP Service", func() {
		reconcile_()
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, nsName(crName+"-cloud-proxy-svc"), &svc)).To(Succeed())
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))
	})

	It("creates owned HTTPProxy with correct customDomains and locations", func() {
		reconcile_()
		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(crName+"-tunnel"), &hp)).To(Succeed())
		Expect(hp.Spec.ClientRef.Name).To(Equal(crClient))
		Expect(hp.Spec.CustomDomains).To(ContainElement(
			fmt.Sprintf("%s-gateway.%s.svc.cluster.local", crServer, ns),
		))
		Expect(hp.Spec.ExtraConfig).To(ContainSubstring("/" + crClient + "/cloud"))
	})

	It("removes owned HTTPProxy on deletion", func() {
		reconcile_()
		var cr tunnelv1alpha1.CloudRestApiProxy
		Expect(k8sClient.Get(ctx, nsName(crName), &cr)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &cr)).To(Succeed())

		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(crName)})
		Expect(err).NotTo(HaveOccurred())

		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(crName+"-tunnel"), &hp)).To(
			Satisfy(errors.IsNotFound))
	})
})
