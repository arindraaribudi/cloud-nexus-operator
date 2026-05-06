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
	// TunnelImage is the default container image used when neither the RegistryProxy
	// spec nor the NexusClient spec sets an explicit image. Populated from TUNNEL_IMAGE env var.
	TunnelImage string
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=httpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=nexusservers,verbs=get;list;watch
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

	// Resolve image: RegistryProxy spec > NexusClient spec > TunnelImage env > hardcoded default.
	image := spoke.Spec.Image.Repository + ":" + spoke.Spec.Image.Tag
	if spoke.Spec.Image.Repository == "" {
		image = r.TunnelImage
	}
	if rp.Spec.Image != nil && rp.Spec.Image.Repository != "" {
		image = rp.Spec.Image.Repository + ":" + rp.Spec.Image.Tag
	}
	if image == "" || image == ":" {
		image = "asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel:1.0.5"
	}
	// Resolve proxy port.
	proxyPort := int32(5000)
	if rp.Spec.ProxyPort != nil {
		proxyPort = *rp.Spec.ProxyPort
	}

	gatewayFQDN := spoke.Status.GatewayFQDN
	gatewayPort := spoke.Status.GatewayPort
	if gatewayFQDN == "" {
		log.Info("gateway not yet discovered, requeuing")
		return ctrl.Result{RequeueAfter: registryProxyRequeueDelay}, nil
	}
	spokeName := spoke.Name

	if err := r.reconcileProxyDeployment(ctx, &rp, image, proxyPort, spokeName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileProxyService(ctx, &rp, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileHTTPProxy(ctx, &rp, proxyPort, spokeName, gatewayFQDN); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.syncStatus(ctx, &rp, gatewayPort, spokeName, gatewayFQDN)
}

func (r *RegistryProxyReconciler) reconcileProxyDeployment(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, image string, proxyPort int32, spokeName string) error {
	replicas := int32(1)
	stripPrefix := "/" + spokeName + "/registry"
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
						Env: []corev1.EnvVar{
							{Name: "STRIP_PREFIX", Value: stripPrefix},
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

func (r *RegistryProxyReconciler) reconcileHTTPProxy(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, proxyPort int32, spokeName, gatewayFQDN string) error {
	localIP := fmt.Sprintf("%s-proxy-svc.%s.svc.cluster.local", rp.Name, rp.Namespace)
	location := "/" + spokeName + "/registry"

	hp := &tunnelv1alpha1.HTTPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-registry-proxy",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, hp, func() error {
		if err := controllerutil.SetControllerReference(rp, hp, r.Scheme); err != nil {
			return err
		}
		if hp.Labels == nil {
			hp.Labels = make(map[string]string)
		}
		hp.Labels["registryproxy"] = rp.Name
		hp.Spec.ClientRef = tunnelv1alpha1.LocalObjectRef{Name: rp.Spec.ClientRef.Name}
		hp.Spec.LocalIP = localIP
		hp.Spec.LocalPort = proxyPort
		hp.Spec.CustomDomains = []string{gatewayFQDN}
		hp.Spec.ExtraConfig = fmt.Sprintf("locations = [%q]", location)
		return nil
	})
	return err
}

func (r *RegistryProxyReconciler) deleteOwnedResources(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy) error {
	hp := &tunnelv1alpha1.HTTPProxy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, hp); err == nil {
		if err := r.Delete(ctx, hp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete HTTPProxy: %w", err)
		}
	}
	return nil
}

func (r *RegistryProxyReconciler) syncStatus(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, vhostPort int32, spokeName, gatewayFQDN string) error {
	var dep appsv1.Deployment
	proxyReady := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-proxy"}, &dep); err == nil {
		proxyReady = dep.Status.ReadyReplicas > 0
	}

	var hp tunnelv1alpha1.HTTPProxy
	tunnelConnected := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, &hp); err == nil {
		tunnelConnected = hp.Status.Phase == "Running"
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
					return
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
		setCondition("TunnelConnected", metav1.ConditionTrue, "HTTPProxyRunning", "")
		updated.Status.HubService = fmt.Sprintf("http://%s:%d/%s/registry", gatewayFQDN, vhostPort, spokeName)
	} else {
		setCondition("TunnelConnected", metav1.ConditionFalse, "HTTPProxyNotRunning", "waiting for FRP tunnel")
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
		// React to HTTPProxy status changes via label-based filtering.
		Watches(
			&tunnelv1alpha1.HTTPProxy{},
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
