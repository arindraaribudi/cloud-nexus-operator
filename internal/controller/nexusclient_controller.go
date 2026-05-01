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
	"tunnel.io/cloud-nexus-operator/internal/config"
)

const (
	// phaseRunning represents the Running phase for NexusClient status.
	phaseRunning = "Running"
)

// NexusClientReconciler reconciles a NexusClient object.
type NexusClientReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *NexusClientReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var spoke tunnelv1alpha1.NexusClient
	if err := r.Get(ctx, req.NamespacedName, &spoke); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve server address.
	serverAddr, serverPort, err := r.resolveServerAddress(ctx, &spoke)
	if err != nil {
		log.Error(err, "failed to resolve server address")
		return ctrl.Result{RequeueAfter: requeueDelay}, err
	}

	// Resolve auth token.
	authToken, err := r.resolveAuthToken(ctx, &spoke)
	if err != nil {
		log.Error(err, "failed to resolve auth token")
		return ctrl.Result{}, err
	}

	// Resolve admin token.
	adminToken, err := r.resolveAdminToken(ctx, &spoke)
	if err != nil {
		log.Error(err, "failed to resolve admin token")
		return ctrl.Result{}, err
	}

	// Include current proxies so the ConfigMap is always complete.
	proxyParams, err := listAllActiveProxyParams(ctx, r.Client, spoke.Namespace, spoke.Name)
	if err != nil {
		log.Error(err, "failed to list proxies")
		return ctrl.Result{}, err
	}
	adminPort := spoke.Spec.Admin.Port
	if adminPort == 0 {
		adminPort = 7400
	}
	base := config.BaseParams{
		ServerAddr:  serverAddr,
		ServerPort:  serverPort,
		AuthToken:   authToken,
		AdminPort:   adminPort,
		AdminToken:  adminToken,
		ExtraConfig: spoke.Spec.ExtraConfig,
	}
	toml, err := config.RenderFrpcToml(base, proxyParams)
	if err != nil {
		log.Error(err, "failed to render frpc.toml")
		return ctrl.Result{}, err
	}

	if err := r.reconcileClientConfigMap(ctx, &spoke, toml); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileClientDeployment(ctx, &spoke); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileAdminService(ctx, &spoke); err != nil {
		return ctrl.Result{}, err
	}

	// Update status.
	updated := spoke.DeepCopy()
	updated.Status.Phase = phaseRunning
	updated.Status.ServerAddress = fmt.Sprintf("%s:%d", serverAddr, serverPort)
	if err := r.Status().Update(ctx, updated); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NexusClientReconciler) resolveServerAddress(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, int32, error) {
	if spoke.Spec.ServerRef.Name != "" {
		var server tunnelv1alpha1.NexusServer
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: spoke.Namespace,
			Name:      spoke.Spec.ServerRef.Name,
		}, &server); err != nil {
			return "", 0, fmt.Errorf("get NexusServer %q: %w", spoke.Spec.ServerRef.Name, err)
		}
		if server.Status.Address == "" {
			return "", 0, fmt.Errorf("NexusServer %q has no address in status yet", spoke.Spec.ServerRef.Name)
		}
		return server.Status.Address, server.Status.Port, nil
	}
	if spoke.Spec.ServerRef.Address != "" {
		return spoke.Spec.ServerRef.Address, spoke.Spec.ServerRef.Port, nil
	}
	return "", 0, fmt.Errorf("serverRef must specify either name or address")
}

func (r *NexusClientReconciler) resolveAuthToken(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, error) {
	if spoke.Spec.AuthTokenSecret != nil {
		return r.readClientSecretKey(ctx, spoke.Namespace, *spoke.Spec.AuthTokenSecret)
	}
	// If spoke references a named server, inherit its auto-generated token.
	if spoke.Spec.ServerRef.Name != "" {
		return r.readClientSecretKey(ctx, spoke.Namespace,
			tunnelv1alpha1.SecretKeyRef{
				Name: spoke.Spec.ServerRef.Name + "-auth-token",
				Key:  "token",
			})
	}
	return ensureSecret(ctx, r.Client, r.Scheme, spoke,
		spoke.Namespace, spoke.Name+"-auth-token", "token")
}

func (r *NexusClientReconciler) resolveAdminToken(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, error) {
	if spoke.Spec.Admin.TokenSecret != nil {
		return r.readClientSecretKey(ctx, spoke.Namespace, *spoke.Spec.Admin.TokenSecret)
	}
	return ensureSecret(ctx, r.Client, r.Scheme, spoke,
		spoke.Namespace, spoke.Name+"-admin-token", "token")
}

func (r *NexusClientReconciler) readClientSecretKey(ctx context.Context, ns string, ref tunnelv1alpha1.SecretKeyRef) (string, error) {
	var s corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: ref.Name}, &s); err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", ns, ref.Name, err)
	}
	val, ok := s.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s has no key %q", ns, ref.Name, ref.Key)
	}
	return string(val), nil
}

func (r *NexusClientReconciler) reconcileClientConfigMap(ctx context.Context, spoke *tunnelv1alpha1.NexusClient, toml string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spoke.Name + "-config",
			Namespace: spoke.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if err := controllerutil.SetControllerReference(spoke, cm, r.Scheme); err != nil {
			return err
		}
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		// Only stamp the sync annotation when content actually changes so
		// the FrpProxy controller's volume-sync timer is not reset needlessly.
		if cm.Data["frpc.toml"] != toml {
			cm.Data["frpc.toml"] = toml
			if cm.Annotations == nil {
				cm.Annotations = make(map[string]string)
			}
			cm.Annotations[configSyncAnnotation] = now
		}
		return nil
	})
	return err
}

func (r *NexusClientReconciler) reconcileClientDeployment(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) error {
	image := spoke.Spec.Image.Repository + ":" + spoke.Spec.Image.Tag
	if spoke.Spec.Image.Repository == "" {
		image = "asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel:1.0.3"
	}

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spoke.Name,
			Namespace: spoke.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		if err := controllerutil.SetControllerReference(spoke, dep, r.Scheme); err != nil {
			return err
		}
		labels := map[string]string{"app": "frpclient", "frpclient": spoke.Name}
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		dep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "frpc",
						Image:   image,
						Command: []string{"/frpc", "-c", "/etc/frp/frpc.toml"},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/frp", ReadOnly: true},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: spoke.Name + "-config"},
							},
						},
					},
				},
			},
		}
		return nil
	})
	return err
}

func (r *NexusClientReconciler) reconcileAdminService(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) error {
	adminPort := spoke.Spec.Admin.Port
	if adminPort == 0 {
		adminPort = 7400
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spoke.Name + "-admin",
			Namespace: spoke.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(spoke, svc, r.Scheme); err != nil {
			return err
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"frpclient": spoke.Name}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "admin",
				Port:       adminPort,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(adminPort),
			},
		}
		return nil
	})
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *NexusClientReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// enqueueFromProxy maps any typed proxy change to the owning NexusClient reconcile request.
	enqueueFromProxy := handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, obj client.Object) []reconcile.Request {
			p, ok := obj.(tunnelv1alpha1.ProxyObject)
			if !ok {
				return nil
			}
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Namespace: obj.GetNamespace(),
					Name:      p.GetClientRef().Name,
				},
			}}
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.NexusClient{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Watches(&tunnelv1alpha1.TCPProxy{}, enqueueFromProxy).
		Watches(&tunnelv1alpha1.UDPProxy{}, enqueueFromProxy).
		Watches(&tunnelv1alpha1.HTTPProxy{}, enqueueFromProxy).
		Watches(&tunnelv1alpha1.HTTPSProxy{}, enqueueFromProxy).
		Watches(&tunnelv1alpha1.TCPMuxProxy{}, enqueueFromProxy).
		Watches(&tunnelv1alpha1.STCPProxy{}, enqueueFromProxy).
		Watches(&tunnelv1alpha1.XTCPProxy{}, enqueueFromProxy).
		Watches(&tunnelv1alpha1.SUDPProxy{}, enqueueFromProxy).
		Named("frpclient").
		Complete(r)
}
