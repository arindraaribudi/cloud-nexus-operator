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
	"strings"
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

const cloudRestApiProxyRequeueDelay = 15 * time.Second

// CloudRestApiProxyReconciler reconciles a CloudRestApiProxy object.
type CloudRestApiProxyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=cloudrestapiproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=cloudrestapiproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=cloudrestapiproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=httpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=nexusclients,verbs=get;list;watch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=nexusservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *CloudRestApiProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cr tunnelv1alpha1.CloudRestApiProxy
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !cr.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer) {
			if err := r.deleteOwnedResources(ctx, &cr); err != nil {
				log.Error(err, "cleanup failed")
				return ctrl.Result{RequeueAfter: cloudRestApiProxyRequeueDelay}, nil
			}
			controllerutil.RemoveFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer)
			return ctrl.Result{}, r.Update(ctx, &cr)
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer) {
		controllerutil.AddFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve NexusClient.
	var spoke tunnelv1alpha1.NexusClient
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: cr.Namespace,
		Name:      cr.Spec.ClientRef.Name,
	}, &spoke); err != nil {
		if errors.IsNotFound(err) {
			log.Info("NexusClient not found, requeuing", "client", cr.Spec.ClientRef.Name)
			return ctrl.Result{RequeueAfter: cloudRestApiProxyRequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	gatewayFQDN := spoke.Status.GatewayFQDN
	gatewayPort := spoke.Status.GatewayPort
	if gatewayFQDN == "" {
		log.Info("gateway not yet discovered, requeuing")
		return ctrl.Result{RequeueAfter: cloudRestApiProxyRequeueDelay}, nil
	}

	proxyPort := cr.Spec.ProxyPort
	if proxyPort == 0 {
		proxyPort = 8080
	}
	image := spoke.Spec.Image.Repository + ":" + spoke.Spec.Image.Tag
	if cr.Spec.Image != nil && cr.Spec.Image.Repository != "" {
		image = cr.Spec.Image.Repository + ":" + cr.Spec.Image.Tag
	}
	spokeName := spoke.Name

	if err := r.reconcileDeployment(ctx, &cr, image, proxyPort, spokeName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileService(ctx, &cr, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileHTTPProxy(ctx, &cr, proxyPort, spokeName, gatewayFQDN); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.syncStatus(ctx, &cr, gatewayPort, spokeName, gatewayFQDN)
}

func (r *CloudRestApiProxyReconciler) reconcileDeployment(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, image string, proxyPort int32, spokeName string) error {
	replicas := int32(1)
	stripPrefix := "/" + spokeName + "/cloud"
	allowedDomains := strings.Join(cr.Spec.AllowedDomains, ",")

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-cloud-proxy",
			Namespace: cr.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		if err := controllerutil.SetControllerReference(cr, dep, r.Scheme); err != nil {
			return err
		}
		labels := map[string]string{"app": "cloud-proxy", "cloudrestapiproxy": cr.Name}
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		dep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				ServiceAccountName: cr.Spec.ServiceAccountName,
				Containers: []corev1.Container{
					{
						Name:    "cloud-proxy",
						Image:   image,
						Command: []string{"/cloud-proxy"},
						Ports: []corev1.ContainerPort{
							{Name: "proxy", ContainerPort: proxyPort, Protocol: corev1.ProtocolTCP},
						},
						Env: []corev1.EnvVar{
							{Name: "STRIP_PREFIX", Value: stripPrefix},
							{Name: "PROXY_PORT", Value: fmt.Sprintf("%d", proxyPort)},
							{Name: "ALLOWED_DOMAINS", Value: allowedDomains},
						},
					},
				},
			},
		}
		return nil
	})
	return err
}

func (r *CloudRestApiProxyReconciler) reconcileService(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, proxyPort int32) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-cloud-proxy-svc",
			Namespace: cr.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(cr, svc, r.Scheme); err != nil {
			return err
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"cloudrestapiproxy": cr.Name}
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

func (r *CloudRestApiProxyReconciler) reconcileHTTPProxy(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, proxyPort int32, spokeName, gatewayFQDN string) error {
	localIP := fmt.Sprintf("%s-cloud-proxy-svc.%s.svc.cluster.local", cr.Name, cr.Namespace)
	location := "/" + spokeName + "/cloud"

	hp := &tunnelv1alpha1.HTTPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-tunnel",
			Namespace: cr.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, hp, func() error {
		if err := controllerutil.SetControllerReference(cr, hp, r.Scheme); err != nil {
			return err
		}
		if hp.Labels == nil {
			hp.Labels = make(map[string]string)
		}
		hp.Labels["cloudrestapiproxy"] = cr.Name
		hp.Spec.ClientRef = tunnelv1alpha1.LocalObjectRef{Name: cr.Spec.ClientRef.Name}
		hp.Spec.LocalIP = localIP
		hp.Spec.LocalPort = proxyPort
		hp.Spec.CustomDomains = []string{gatewayFQDN}
		hp.Spec.ExtraConfig = fmt.Sprintf("locations = [%q]", location)
		return nil
	})
	return err
}

func (r *CloudRestApiProxyReconciler) deleteOwnedResources(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy) error {
	hp := &tunnelv1alpha1.HTTPProxy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name + "-tunnel"}, hp); err == nil {
		if err := r.Delete(ctx, hp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete HTTPProxy: %w", err)
		}
	}
	return nil
}

func (r *CloudRestApiProxyReconciler) syncStatus(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, vhostPort int32, spokeName, gatewayFQDN string) error {
	var dep appsv1.Deployment
	proxyReady := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name + "-cloud-proxy"}, &dep); err == nil {
		proxyReady = dep.Status.ReadyReplicas > 0
	}

	var hp tunnelv1alpha1.HTTPProxy
	tunnelConnected := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name + "-tunnel"}, &hp); err == nil {
		tunnelConnected = hp.Status.Phase == "Running"
	}

	updated := cr.DeepCopy()
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
		setCondition("ProxyReady", metav1.ConditionFalse, "DeploymentNotReady", "waiting for cloud-proxy pod")
	}
	if tunnelConnected {
		setCondition("TunnelConnected", metav1.ConditionTrue, "HTTPProxyRunning", "")
		updated.Status.GatewayURL = fmt.Sprintf("http://%s:%d/%s/cloud", gatewayFQDN, vhostPort, spokeName)
	} else {
		setCondition("TunnelConnected", metav1.ConditionFalse, "HTTPProxyNotRunning", "waiting for FRP tunnel")
		updated.Status.GatewayURL = ""
	}

	if proxyReady && tunnelConnected {
		updated.Status.Phase = "Running"
	} else {
		updated.Status.Phase = "Pending"
	}

	return r.Status().Update(ctx, updated)
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudRestApiProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.CloudRestApiProxy{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(
			&tunnelv1alpha1.HTTPProxy{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				crName, ok := obj.GetLabels()["cloudrestapiproxy"]
				if !ok || crName == "" {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      crName,
					},
				}}
			}),
		).
		Named("cloudrestapiproxy").
		Complete(r)
}
