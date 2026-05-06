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
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
	"tunnel.io/cloud-nexus-operator/internal/config"
)

// NexusServerReconciler reconciles a NexusServer object.
type NexusServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// WebhookServiceAddr is the FQDN:port of the frps-webhook Service used as the
	// default [[httpPlugins]] addr when NexusServer.spec.operatorWebhookAddr is empty.
	// When unset, falls back to controller-manager-frps-plugin.<server-namespace>.svc.cluster.local:9090.
	WebhookServiceAddr string
	// TunnelImage is the default frps container image used when NexusServer.spec.image
	// is not set. Populated from the TUNNEL_IMAGE env var on the manager Deployment.
	TunnelImage string
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *NexusServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var server tunnelv1alpha1.NexusServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve auth token.
	authToken, err := r.resolveAuthToken(ctx, &server)
	if err != nil {
		log.Error(err, "failed to resolve auth token")
		return ctrl.Result{}, err
	}

	// Resolve dashboard password.
	dashPassword := ""
	if server.Spec.DashboardPasswordSecret != nil {
		dashPassword, err = r.readSecretKey(ctx, server.Namespace, *server.Spec.DashboardPasswordSecret)
		if err != nil {
			log.Error(err, "failed to read dashboard password secret")
			return ctrl.Result{}, err
		}
	}

	// Render frps.toml.
	defaultWebhookAddr := r.WebhookServiceAddr
	if defaultWebhookAddr == "" {
		defaultWebhookAddr = fmt.Sprintf("controller-manager-frps-plugin.%s.svc.cluster.local:9090", server.Namespace)
	}
	toml, err := config.RenderFrpsToml(server.Spec, authToken, dashPassword, defaultWebhookAddr)
	if err != nil {
		log.Error(err, "failed to render frps.toml")
		return ctrl.Result{}, err
	}

	if err := r.reconcileConfigMap(ctx, &server, toml); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileDeployment(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileService(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileGatewayService(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NexusServerReconciler) resolveAuthToken(ctx context.Context, server *tunnelv1alpha1.NexusServer) (string, error) {
	if server.Spec.AuthTokenSecret != nil {
		return r.readSecretKey(ctx, server.Namespace, *server.Spec.AuthTokenSecret)
	}
	return ensureSecret(ctx, r.Client, r.Scheme, server,
		server.Namespace, server.Name+"-auth-token", "token")
}

func (r *NexusServerReconciler) readSecretKey(ctx context.Context, ns string, ref tunnelv1alpha1.SecretKeyRef) (string, error) {
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

func (r *NexusServerReconciler) reconcileConfigMap(ctx context.Context, server *tunnelv1alpha1.NexusServer, toml string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name + "-config",
			Namespace: server.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if err := controllerutil.SetControllerReference(server, cm, r.Scheme); err != nil {
			return err
		}
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		cm.Data["frps.toml"] = toml
		return nil
	})
	return err
}

func (r *NexusServerReconciler) reconcileDeployment(ctx context.Context, server *tunnelv1alpha1.NexusServer) error {
	replicas := server.Spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	image := server.Spec.Image.Repository + ":" + server.Spec.Image.Tag
	if server.Spec.Image.Repository == "" {
		if r.TunnelImage != "" {
			image = r.TunnelImage
		} else {
			image = "asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel:1.0.3"
		}
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name,
			Namespace: server.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		if err := controllerutil.SetControllerReference(server, dep, r.Scheme); err != nil {
			return err
		}
		labels := map[string]string{"app": "frpserver", "frpserver": server.Name}
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		containers := []corev1.Container{
			{
				Name:    "frps",
				Image:   image,
				Command: []string{"/frps", "-c", "/etc/frp/frps.toml"},
				Ports: []corev1.ContainerPort{
					{Name: "bind", ContainerPort: server.Spec.BindPort, Protocol: corev1.ProtocolTCP},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "config", MountPath: "/etc/frp", ReadOnly: true},
				},
			},
		}
		if server.Spec.DiscoveryPort > 0 {
			gatewayFQDN := fmt.Sprintf("%s-gateway.%s.svc.cluster.local", server.Name, server.Namespace)
			containers = append(containers, corev1.Container{
				Name:    "nexus-discovery",
				Image:   image,
				Command: []string{"/nexus-discovery"},
				Ports: []corev1.ContainerPort{
					{Name: "discovery", ContainerPort: server.Spec.DiscoveryPort, Protocol: corev1.ProtocolTCP},
				},
				Env: []corev1.EnvVar{
					{Name: "DISCOVERY_PORT", Value: fmt.Sprint(server.Spec.DiscoveryPort)},
					{Name: "GATEWAY_FQDN", Value: gatewayFQDN},
					{Name: "GATEWAY_PORT", Value: fmt.Sprint(server.Spec.VhostHTTPPort)},
					{Name: "SERVER_NAME", Value: server.Name},
					{Name: "SERVER_NAMESPACE", Value: server.Namespace},
				},
			})
		}
		dep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				Containers: containers,
				Volumes: []corev1.Volume{
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: server.Name + "-config"},
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

func (r *NexusServerReconciler) reconcileService(ctx context.Context, server *tunnelv1alpha1.NexusServer) error {
	svcType := server.Spec.Service.Type
	if svcType == "" {
		svcType = corev1.ServiceTypeLoadBalancer
	}

	ports := server.Spec.Service.Ports
	if len(ports) == 0 {
		bindPort := server.Spec.BindPort
		if bindPort == 0 {
			bindPort = 7000
		}
		ports = []corev1.ServicePort{
			{Name: "frp", Port: bindPort, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(bindPort)},
		}
	}

	if server.Spec.DiscoveryPort > 0 {
		ports = append(ports, corev1.ServicePort{
			Name:       "discovery",
			Port:       server.Spec.DiscoveryPort,
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromInt32(server.Spec.DiscoveryPort),
		})
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name,
			Namespace: server.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(server, svc, r.Scheme); err != nil {
			return err
		}
		// Preserve node ports assigned by Kubernetes to avoid constant diffs.
		existingNodePorts := map[string]int32{}
		for _, p := range svc.Spec.Ports {
			if p.NodePort != 0 {
				existingNodePorts[p.Name] = p.NodePort
			}
		}
		merged := make([]corev1.ServicePort, len(ports))
		for i, p := range ports {
			// Default targetPort to port value if unset.
			if p.TargetPort == (intstr.IntOrString{}) {
				p.TargetPort = intstr.FromInt32(p.Port)
			}
			if np, ok := existingNodePorts[p.Name]; ok {
				p.NodePort = np
			}
			merged[i] = p
		}
		// Merge spec annotations into existing ones — don't remove annotations
		// added by GKE/Kubernetes controllers (e.g. cloud.google.com/neg-status).
		if svc.Annotations == nil {
			svc.Annotations = make(map[string]string)
		}
		for k, v := range server.Spec.Service.Annotations {
			svc.Annotations[k] = v
		}
		svc.Spec.Type = svcType
		svc.Spec.Selector = map[string]string{"frpserver": server.Name}
		svc.Spec.Ports = merged
		return nil
	})
	if err != nil {
		return err
	}

	// Compute desired status.
	newAddr := ""
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) > 0 {
		ing := svc.Status.LoadBalancer.Ingress[0]
		if ing.IP != "" {
			newAddr = ing.IP
		} else {
			newAddr = ing.Hostname
		}
	}
	newPort := server.Spec.BindPort
	if newPort == 0 {
		newPort = 7000
	}
	// Only write status when it actually changed to avoid spurious reconciles.
	if server.Status.Address == newAddr && server.Status.Port == newPort {
		return nil
	}
	updated := server.DeepCopy()
	updated.Status.Address = newAddr
	updated.Status.Port = newPort
	return r.Status().Update(ctx, updated)
}

func (r *NexusServerReconciler) reconcileGatewayService(ctx context.Context, server *tunnelv1alpha1.NexusServer) error {
	if server.Spec.VhostHTTPPort == 0 {
		return nil
	}
	svcType := server.Spec.GatewayServiceType
	if svcType == "" {
		svcType = corev1.ServiceTypeClusterIP
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name + "-gateway",
			Namespace: server.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(server, svc, r.Scheme); err != nil {
			return err
		}
		svc.Spec.Type = svcType
		svc.Spec.Selector = map[string]string{"frpserver": server.Name}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "vhost-http",
				Port:       server.Spec.VhostHTTPPort,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(server.Spec.VhostHTTPPort),
			},
		}
		return nil
	})
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *NexusServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.NexusServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Named("frpserver").
		Complete(r)
}

// Ensure svc is used (imported types).
var _ = &corev1.Service{}
var _ = errors.IsNotFound
