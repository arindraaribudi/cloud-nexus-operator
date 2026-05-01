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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

const registryProxyRequeueDelay = 15 * time.Second

// RegistryProxyReconciler reconciles a RegistryProxy object.
type RegistryProxyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=tcpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *RegistryProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var rp tunnelv1alpha1.RegistryProxy
	if err := r.Get(ctx, req.NamespacedName, &rp); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !rp.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&rp, tunnelv1alpha1.RegistryCleanupFinalizer) {
			if err := r.deleteOwnedResources(ctx, &rp); err != nil {
				log.Error(err, "cleanup failed")
				return ctrl.Result{RequeueAfter: registryProxyRequeueDelay}, nil
			}
			controllerutil.RemoveFinalizer(&rp, tunnelv1alpha1.RegistryCleanupFinalizer)
			return ctrl.Result{}, r.Update(ctx, &rp)
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(&rp, tunnelv1alpha1.RegistryCleanupFinalizer) {
		controllerutil.AddFinalizer(&rp, tunnelv1alpha1.RegistryCleanupFinalizer)
		if err := r.Update(ctx, &rp); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve NexusClient.
	var spoke tunnelv1alpha1.NexusClient
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: rp.Namespace,
		Name:      rp.Spec.ClientRef.Name,
	}, &spoke); err != nil {
		if errors.IsNotFound(err) {
			log.Info("NexusClient not found, requeuing", "client", rp.Spec.ClientRef.Name)
			return ctrl.Result{RequeueAfter: registryProxyRequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve image: use spec.Image if set, else inherit from NexusClient.
	image := spoke.Spec.Image.Repository + ":" + spoke.Spec.Image.Tag
	if rp.Spec.Image != nil && rp.Spec.Image.Repository != "" {
		image = rp.Spec.Image.Repository + ":" + rp.Spec.Image.Tag
	}
	// Resolve proxy port.
	proxyPort := int32(5000)
	if rp.Spec.ProxyPort != nil {
		proxyPort = *rp.Spec.ProxyPort
	}

	// Resolve frpServerName for metadata (used by hub webhook to create Service selector).
	frpServerName := spoke.Spec.ServerRef.Name // may be "" for explicit-address mode

	if err := r.reconcileProxyDeployment(ctx, &rp, image, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileProxyService(ctx, &rp, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileTCPProxy(ctx, &rp, proxyPort, frpServerName); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.syncStatus(ctx, &rp)
}

func (r *RegistryProxyReconciler) reconcileProxyDeployment(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, image string, proxyPort int32) error {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-proxy",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		if err := controllerutil.SetControllerReference(rp, dep, r.Scheme); err != nil {
			return err
		}
		labels := map[string]string{"app": "registry-proxy", "registryproxy": rp.Name}
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		dep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				ServiceAccountName: rp.Spec.ServiceAccountName,
				Containers: []corev1.Container{
					{
						Name:    "registry-proxy",
						Image:   image,
						Command: []string{"/registry-proxy", fmt.Sprintf("--port=%d", proxyPort)},
						Ports: []corev1.ContainerPort{
							{Name: "proxy", ContainerPort: proxyPort, Protocol: corev1.ProtocolTCP},
						},
					},
				},
			},
		}
		return nil
	})
	return err
}

func (r *RegistryProxyReconciler) reconcileProxyService(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, proxyPort int32) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-proxy-svc",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(rp, svc, r.Scheme); err != nil {
			return err
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"registryproxy": rp.Name}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "proxy",
				Port:       proxyPort,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(proxyPort),
			},
		}
		return nil
	})
	return err
}

func (r *RegistryProxyReconciler) reconcileTCPProxy(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, proxyPort int32, frpServerName string) error {
	localIP := fmt.Sprintf("%s-proxy-svc.%s.svc.cluster.local", rp.Name, rp.Namespace)
	extraConfig := fmt.Sprintf(
		"metadatas.svcName = %q\nmetadatas.namespace = %q\nmetadatas.remotePort = %q\nmetadatas.frpServerName = %q",
		rp.Name+"-docker",
		rp.Namespace,
		fmt.Sprintf("%d", rp.Spec.RemotePort),
		frpServerName,
	)

	fp := &tunnelv1alpha1.TCPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-registry-proxy",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, fp, func() error {
		if err := controllerutil.SetControllerReference(rp, fp, r.Scheme); err != nil {
			return err
		}
		if fp.Labels == nil {
			fp.Labels = make(map[string]string)
		}
		fp.Labels["registryproxy"] = rp.Name
		fp.Spec.ClientRef = tunnelv1alpha1.LocalObjectRef{Name: rp.Spec.ClientRef.Name}
		fp.Spec.LocalIP = localIP
		fp.Spec.LocalPort = proxyPort
		fp.Spec.RemotePort = rp.Spec.RemotePort
		fp.Spec.ExtraConfig = extraConfig
		return nil
	})
	return err
}

func (r *RegistryProxyReconciler) deleteOwnedResources(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy) error {
	// Delete TCPProxy first so frpc hot-reloads before we remove the proxy pod.
	fp := &tunnelv1alpha1.TCPProxy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, fp); err == nil {
		if err := r.Delete(ctx, fp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete TCPProxy: %w", err)
		}
	}
	// Owned Deployment and Service are garbage-collected via ownerReferences.
	return nil
}

func (r *RegistryProxyReconciler) syncStatus(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy) error {
	// Check proxy Deployment readiness.
	var dep appsv1.Deployment
	proxyReady := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-proxy"}, &dep); err == nil {
		proxyReady = dep.Status.ReadyReplicas > 0
	}

	// Check tunnel connectivity via TCPProxy phase.
	var fp tunnelv1alpha1.TCPProxy
	tunnelConnected := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, &fp); err == nil {
		tunnelConnected = fp.Status.Phase == "Running"
	}

	updated := rp.DeepCopy()
	now := metav1.Now()

	setCondition := func(condType string, status metav1.ConditionStatus, reason, msg string) {
		cond := metav1.Condition{
			Type:               condType,
			Status:             status,
			Reason:             reason,
			Message:            msg,
			LastTransitionTime: now,
		}
		for i, c := range updated.Status.Conditions {
			if c.Type == condType {
				if c.Status == status {
					return // no change
				}
				updated.Status.Conditions[i] = cond
				return
			}
		}
		updated.Status.Conditions = append(updated.Status.Conditions, cond)
	}

	if proxyReady {
		setCondition("ProxyReady", metav1.ConditionTrue, "DeploymentReady", "")
	} else {
		setCondition("ProxyReady", metav1.ConditionFalse, "DeploymentNotReady", "waiting for registry-proxy pod")
	}
	if tunnelConnected {
		setCondition("TunnelConnected", metav1.ConditionTrue, "FrpProxyRunning", "")
		updated.Status.HubService = fmt.Sprintf("%s-docker.%s.svc.cluster.local", rp.Name, rp.Namespace)
	} else {
		setCondition("TunnelConnected", metav1.ConditionFalse, "FrpProxyNotRunning", "waiting for FRP tunnel")
		updated.Status.HubService = ""
	}

	if proxyReady && tunnelConnected {
		updated.Status.Phase = "Running"
	} else {
		updated.Status.Phase = "Pending"
	}

	return r.Status().Update(ctx, updated)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RegistryProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.RegistryProxy{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		// React to TCPProxy status changes via label-based filtering.
		Watches(
			&tunnelv1alpha1.TCPProxy{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				rpName, ok := obj.GetLabels()["registryproxy"]
				if !ok || rpName == "" {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      rpName,
					},
				}}
			}),
		).
		Named("registryproxy").
		Complete(r)
}
