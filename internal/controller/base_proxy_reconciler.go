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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
	"tunnel.io/cloud-nexus-operator/internal/config"
	"tunnel.io/cloud-nexus-operator/internal/reload"
)

// BaseProxyReconciler contains the common reconcile logic for all 8 typed proxy CRDs.
type BaseProxyReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ReloadClient *reload.Client
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=tcpproxies;udpproxies;httpproxies;httpsproxies;tcpmuxproxies;stcpproxies;xtcpproxies;sudpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=tcpproxies/status;udpproxies/status;httpproxies/status;httpsproxies/status;tcpmuxproxies/status;stcpproxies/status;xtcpproxies/status;sudpproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=tcpproxies/finalizers;udpproxies/finalizers;httpproxies/finalizers;httpsproxies/finalizers;tcpmuxproxies/finalizers;stcpproxies/finalizers;xtcpproxies/finalizers;sudpproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

// RegisterProxyIndexers registers field indexers for all 8 typed proxy CRDs.
// This must be called once during manager setup.
func RegisterProxyIndexers(mgr ctrl.Manager) error {
	indexField := "spec.clientRef.name"

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.TCPProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.TCPProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.UDPProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.UDPProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.HTTPProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.HTTPProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.HTTPSProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.HTTPSProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.TCPMuxProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.TCPMuxProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.STCPProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.STCPProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.XTCPProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.XTCPProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.SUDPProxy{}, indexField,
		func(obj client.Object) []string {
			return []string{obj.(*tunnelv1alpha1.SUDPProxy).Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	return nil
}

// proxyObjectToParams converts a ProxyObject to ProxyParams for config rendering.
func proxyObjectToParams(obj tunnelv1alpha1.ProxyObject) config.ProxyParams {
	localIP := obj.GetLocalIP()
	if localIP == "" {
		localIP = "127.0.0.1"
	}
	return config.ProxyParams{
		Name:          obj.GetName(),
		Type:          obj.GetProxyType(),
		LocalIP:       localIP,
		LocalPort:     obj.GetLocalPort(),
		RemotePort:    obj.GetRemotePort(),
		CustomDomains: obj.GetCustomDomains(),
		Subdomain:     obj.GetSubdomain(),
		SecretKey:     obj.GetSecretKey(),
		AllowUsers:    obj.GetAllowUsers(),
		Multiplexer:   obj.GetMultiplexer(),
		ExtraConfig:   obj.GetExtraConfig(),
	}
}

// listAllActiveProxyParams lists all active proxies (all 8 types) for the given client.
func listAllActiveProxyParams(ctx context.Context, c client.Client, namespace, clientName string) ([]config.ProxyParams, error) {
	indexField := "spec.clientRef.name"
	var result []config.ProxyParams

	// TCP proxies
	var tcpList tunnelv1alpha1.TCPProxyList
	if err := c.List(ctx, &tcpList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list TCPProxy: %w", err)
	}
	for _, p := range tcpList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	// UDP proxies
	var udpList tunnelv1alpha1.UDPProxyList
	if err := c.List(ctx, &udpList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list UDPProxy: %w", err)
	}
	for _, p := range udpList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	// HTTP proxies
	var httpList tunnelv1alpha1.HTTPProxyList
	if err := c.List(ctx, &httpList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list HTTPProxy: %w", err)
	}
	for _, p := range httpList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	// HTTPS proxies
	var httpsList tunnelv1alpha1.HTTPSProxyList
	if err := c.List(ctx, &httpsList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list HTTPSProxy: %w", err)
	}
	for _, p := range httpsList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	// TCPMux proxies
	var tcpmuxList tunnelv1alpha1.TCPMuxProxyList
	if err := c.List(ctx, &tcpmuxList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list TCPMuxProxy: %w", err)
	}
	for _, p := range tcpmuxList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	// STCP proxies
	var stcpList tunnelv1alpha1.STCPProxyList
	if err := c.List(ctx, &stcpList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list STCPProxy: %w", err)
	}
	for _, p := range stcpList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	// XTCP proxies
	var xtcpList tunnelv1alpha1.XTCPProxyList
	if err := c.List(ctx, &xtcpList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list XTCPProxy: %w", err)
	}
	for _, p := range xtcpList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	// SUDP proxies
	var sudpList tunnelv1alpha1.SUDPProxyList
	if err := c.List(ctx, &sudpList,
		client.InNamespace(namespace),
		client.MatchingFields{indexField: clientName},
	); err != nil {
		return nil, fmt.Errorf("list SUDPProxy: %w", err)
	}
	for _, p := range sudpList.Items {
		if p.DeletionTimestamp.IsZero() {
			result = append(result, proxyObjectToParams(&p))
		}
	}

	return result, nil
}

// Reconcile implements the common reconcile logic for all proxy types.
func (r *BaseProxyReconciler) Reconcile(ctx context.Context, obj tunnelv1alpha1.ProxyObject) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Handle deletion with finalizer.
	if !obj.GetDeletionTimestamp().IsZero() {
		if controllerutil.ContainsFinalizer(obj, tunnelv1alpha1.ProxyCleanupFinalizer) {
			result, err := r.rebuildAndReload(ctx, obj)
			if err != nil {
				log.Error(err, "reload on deletion failed, will retry")
				return ctrl.Result{RequeueAfter: requeueDelay}, nil
			}
			if result.RequeueAfter > 0 {
				// Waiting for kubelet volume sync; don't remove finalizer yet.
				return result, nil
			}
			controllerutil.RemoveFinalizer(obj, tunnelv1alpha1.ProxyCleanupFinalizer)
			return ctrl.Result{}, r.Update(ctx, obj)
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(obj, tunnelv1alpha1.ProxyCleanupFinalizer) {
		controllerutil.AddFinalizer(obj, tunnelv1alpha1.ProxyCleanupFinalizer)
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	result, err := r.rebuildAndReload(ctx, obj)
	if err != nil {
		log.Error(err, "rebuild and reload failed")
		_ = r.setStatus(ctx, obj, "Failed", err.Error())
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}
	if result.RequeueAfter > 0 {
		// Config written; waiting for kubelet volume sync before reload.
		return result, nil
	}

	return ctrl.Result{}, r.setStatus(ctx, obj, phaseRunning, "")
}

// rebuildAndReload re-renders the full frpc.toml for the parent client,
// updates the ConfigMap if needed, and triggers a hot reload via the frpc
// admin API.
//
// To avoid a race between the ConfigMap write and the kubelet syncing the
// mounted volume into the frpc pod, the function is two-phase:
//
//  1. If the ConfigMap content changed, write it (stamping
//     configSyncAnnotation with the current time) and return a requeue
//     result for configSyncDelay – do NOT reload yet.
//
//  2. On the subsequent reconcile the content is already correct.  Read the
//     annotation; if less than configSyncDelay has elapsed, requeue for the
//     remainder.  Once the window has passed, trigger the admin reload.
func (r *BaseProxyReconciler) rebuildAndReload(ctx context.Context, obj tunnelv1alpha1.ProxyObject) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch parent NexusClient.
	var spoke tunnelv1alpha1.NexusClient
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetClientRef().Name,
	}, &spoke); err != nil {
		return ctrl.Result{}, fmt.Errorf("get NexusClient %q: %w", obj.GetClientRef().Name, err)
	}

	// Resolve server address.
	serverAddr, serverPort, err := r.resolveServerAddress(ctx, &spoke)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Resolve auth token.
	authToken, err := r.resolveAuthToken(ctx, &spoke)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Resolve admin token.
	adminToken, err := r.resolveAdminToken(ctx, &spoke)
	if err != nil {
		return ctrl.Result{}, err
	}

	// List all active proxies for this client.
	proxyParams, err := listAllActiveProxyParams(ctx, r.Client, obj.GetNamespace(), spoke.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("list proxies for client %q: %w", spoke.Name, err)
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
		return ctrl.Result{}, fmt.Errorf("render frpc.toml: %w", err)
	}

	// Phase 1 – write ConfigMap only when content changed.
	// Track whether we actually mutate the object inside the closure.
	contentChanged := false
	now := time.Now().UTC().Format(time.RFC3339)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spoke.Name + "-config",
			Namespace: spoke.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		if cm.Data["frpc.toml"] != toml {
			contentChanged = true
			cm.Data["frpc.toml"] = toml
			if cm.Annotations == nil {
				cm.Annotations = make(map[string]string)
			}
			cm.Annotations[configSyncAnnotation] = now
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("update frpc ConfigMap: %w", err)
	}

	// If we just wrote new content, requeue and let the kubelet sync the
	// volume before we attempt the reload.
	if contentChanged {
		log.Info("config updated; waiting for kubelet volume sync before reload",
			"configMap", cm.Name, "delay", configSyncDelay)
		return ctrl.Result{RequeueAfter: configSyncDelay}, nil
	}

	// Phase 2 – content is already correct.  Check how long ago the last
	// write was; if within the sync window, wait for the remainder.
	if ts := cm.Annotations[configSyncAnnotation]; ts != "" {
		updatedAt, parseErr := time.Parse(time.RFC3339, ts)
		if parseErr == nil {
			if elapsed := time.Since(updatedAt); elapsed < configSyncDelay {
				remaining := configSyncDelay - elapsed
				log.Info("skipping admin reload: waiting for kubelet volume sync",
					"elapsed", elapsed.Round(time.Second), "remaining", remaining.Round(time.Second))
				return ctrl.Result{RequeueAfter: remaining}, nil
			}
		}
	}

	// Phase 2 complete – the volume has had time to sync.  Trigger reload.
	adminHost := spoke.Name + "-admin." + spoke.Namespace + ".svc"

	rc := r.ReloadClient
	if rc == nil {
		rc = reload.New(nil)
	}
	if err := rc.Reload(ctx, adminHost, adminPort, adminToken); err != nil {
		return ctrl.Result{}, fmt.Errorf("frpc admin reload: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *BaseProxyReconciler) resolveServerAddress(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, int32, error) {
	if spoke.Spec.ServerRef.Name != "" {
		var server tunnelv1alpha1.NexusServer
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: spoke.Namespace,
			Name:      spoke.Spec.ServerRef.Name,
		}, &server); err != nil {
			return "", 0, fmt.Errorf("get NexusServer %q: %w", spoke.Spec.ServerRef.Name, err)
		}
		return server.Status.Address, server.Status.Port, nil
	}
	return spoke.Spec.ServerRef.Address, spoke.Spec.ServerRef.Port, nil
}

func (r *BaseProxyReconciler) resolveAuthToken(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, error) {
	if spoke.Spec.AuthTokenSecret != nil {
		return r.readSecretKey(ctx, spoke.Namespace, *spoke.Spec.AuthTokenSecret)
	}
	if spoke.Spec.ServerRef.Name != "" {
		return r.readSecretKey(ctx, spoke.Namespace,
			tunnelv1alpha1.SecretKeyRef{
				Name: spoke.Spec.ServerRef.Name + "-auth-token",
				Key:  "token",
			})
	}
	return r.readSecretKey(ctx, spoke.Namespace,
		tunnelv1alpha1.SecretKeyRef{Name: spoke.Name + "-auth-token", Key: "token"})
}

func (r *BaseProxyReconciler) resolveAdminToken(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, error) {
	if spoke.Spec.Admin.TokenSecret != nil {
		return r.readSecretKey(ctx, spoke.Namespace, *spoke.Spec.Admin.TokenSecret)
	}
	return r.readSecretKey(ctx, spoke.Namespace,
		tunnelv1alpha1.SecretKeyRef{Name: spoke.Name + "-admin-token", Key: "token"})
}

func (r *BaseProxyReconciler) readSecretKey(ctx context.Context, ns string, ref tunnelv1alpha1.SecretKeyRef) (string, error) {
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

func (r *BaseProxyReconciler) setStatus(ctx context.Context, obj tunnelv1alpha1.ProxyObject, phase, msg string) error {
	updated := obj.DeepCopyObject().(tunnelv1alpha1.ProxyObject)
	status := updated.GetProxyStatus()
	status.Phase = phase
	now := metav1.Now()
	if phase == phaseRunning {
		status.LastReloadTime = &now
	}
	condStatus := metav1.ConditionTrue
	if phase != phaseRunning {
		condStatus = metav1.ConditionFalse
	}
	cond := metav1.Condition{
		Type:               "Synced",
		Status:             condStatus,
		Reason:             "ReloadSucceeded",
		Message:            msg,
		LastTransitionTime: now,
	}
	if phase != phaseRunning {
		cond.Reason = "ReloadFailed"
	}
	// Update or append condition.
	found := false
	for i, c := range status.Conditions {
		if c.Type == "Synced" {
			status.Conditions[i] = cond
			found = true
			break
		}
	}
	if !found {
		status.Conditions = append(status.Conditions, cond)
	}
	return r.Status().Update(ctx, updated)
}
