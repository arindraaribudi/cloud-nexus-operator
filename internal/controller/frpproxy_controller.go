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
	"k8s.io/apimachinery/pkg/api/errors"
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

const (
	requeueDelay = 10 * time.Second

	// configSyncDelay is how long we wait after writing a ConfigMap before
	// triggering the frpc hot-reload. The kubelet syncs projected ConfigMap
	// volumes on a ~1-minute cycle; 30 s gives a comfortable margin on GKE
	// where watch-driven updates typically propagate within 10–20 s.
	configSyncDelay = 30 * time.Second

	// configSyncAnnotation records the UTC time at which the frpc ConfigMap
	// content was last changed. The FrpProxy controller reads it to decide
	// whether the kubelet has had enough time to sync the volume.
	configSyncAnnotation = "tunnel.io/config-updated-at"
)

// FrpProxyReconciler reconciles a FrpProxy object.
type FrpProxyReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ReloadClient *reload.Client
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *FrpProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var proxy tunnelv1alpha1.FrpProxy
	if err := r.Get(ctx, req.NamespacedName, &proxy); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer.
	if !proxy.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&proxy, tunnelv1alpha1.ProxyCleanupFinalizer) {
			result, err := r.rebuildAndReload(ctx, &proxy)
			if err != nil {
				log.Error(err, "reload on deletion failed, will retry")
				return ctrl.Result{RequeueAfter: requeueDelay}, nil
			}
			if result.RequeueAfter > 0 {
				// Waiting for kubelet volume sync; don't remove finalizer yet.
				return result, nil
			}
			controllerutil.RemoveFinalizer(&proxy, tunnelv1alpha1.ProxyCleanupFinalizer)
			return ctrl.Result{}, r.Update(ctx, &proxy)
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(&proxy, tunnelv1alpha1.ProxyCleanupFinalizer) {
		controllerutil.AddFinalizer(&proxy, tunnelv1alpha1.ProxyCleanupFinalizer)
		if err := r.Update(ctx, &proxy); err != nil {
			return ctrl.Result{}, err
		}
	}

	result, err := r.rebuildAndReload(ctx, &proxy)
	if err != nil {
		log.Error(err, "rebuild and reload failed")
		_ = r.setStatus(ctx, &proxy, "Failed", err.Error())
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}
	if result.RequeueAfter > 0 {
		// Config written; waiting for kubelet volume sync before reload.
		return result, nil
	}

	return ctrl.Result{}, r.setStatus(ctx, &proxy, phaseRunning, "")
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
func (r *FrpProxyReconciler) rebuildAndReload(ctx context.Context, proxy *tunnelv1alpha1.FrpProxy) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch parent NexusClient.
	var spoke tunnelv1alpha1.NexusClient
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: proxy.Namespace,
		Name:      proxy.Spec.ClientRef.Name,
	}, &spoke); err != nil {
		return ctrl.Result{}, fmt.Errorf("get NexusClient %q: %w", proxy.Spec.ClientRef.Name, err)
	}

	// Resolve server address.
	serverAddr, serverPort, err := r.resolveProxyServerAddress(ctx, &spoke)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Resolve auth token.
	authToken, err := r.resolveProxyAuthToken(ctx, &spoke)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Resolve admin token.
	adminToken, err := r.resolveProxyAdminToken(ctx, &spoke)
	if err != nil {
		return ctrl.Result{}, err
	}

	// List all active proxies for this client.
	var allProxies tunnelv1alpha1.FrpProxyList
	if err := r.List(ctx, &allProxies,
		client.InNamespace(proxy.Namespace),
		client.MatchingFields{"spec.clientRef.name": spoke.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list FrpProxy for client %q: %w", spoke.Name, err)
	}

	// Exclude proxies being deleted.
	var activeProxies []tunnelv1alpha1.FrpProxy
	for _, p := range allProxies.Items {
		if p.DeletionTimestamp.IsZero() {
			activeProxies = append(activeProxies, p)
		}
	}

	toml, err := config.RenderFrpcToml(spoke.Spec, serverAddr, serverPort, authToken, adminToken, activeProxies)
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
	adminPort := spoke.Spec.Admin.Port
	if adminPort == 0 {
		adminPort = 7400
	}
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

func (r *FrpProxyReconciler) resolveProxyServerAddress(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, int32, error) {
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

func (r *FrpProxyReconciler) resolveProxyAuthToken(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, error) {
	if spoke.Spec.AuthTokenSecret != nil {
		return r.readProxySecretKey(ctx, spoke.Namespace, *spoke.Spec.AuthTokenSecret)
	}
	if spoke.Spec.ServerRef.Name != "" {
		return r.readProxySecretKey(ctx, spoke.Namespace,
			tunnelv1alpha1.SecretKeyRef{
				Name: spoke.Spec.ServerRef.Name + "-auth-token",
				Key:  "token",
			})
	}
	return r.readProxySecretKey(ctx, spoke.Namespace,
		tunnelv1alpha1.SecretKeyRef{Name: spoke.Name + "-auth-token", Key: "token"})
}

func (r *FrpProxyReconciler) resolveProxyAdminToken(ctx context.Context, spoke *tunnelv1alpha1.NexusClient) (string, error) {
	if spoke.Spec.Admin.TokenSecret != nil {
		return r.readProxySecretKey(ctx, spoke.Namespace, *spoke.Spec.Admin.TokenSecret)
	}
	return r.readProxySecretKey(ctx, spoke.Namespace,
		tunnelv1alpha1.SecretKeyRef{Name: spoke.Name + "-admin-token", Key: "token"})
}

func (r *FrpProxyReconciler) readProxySecretKey(ctx context.Context, ns string, ref tunnelv1alpha1.SecretKeyRef) (string, error) {
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

func (r *FrpProxyReconciler) setStatus(ctx context.Context, proxy *tunnelv1alpha1.FrpProxy, phase, msg string) error {
	updated := proxy.DeepCopy()
	updated.Status.Phase = phase
	now := metav1.Now()
	if phase == phaseRunning {
		updated.Status.LastReloadTime = &now
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
	for i, c := range updated.Status.Conditions {
		if c.Type == "Synced" {
			updated.Status.Conditions[i] = cond
			found = true
			break
		}
	}
	if !found {
		updated.Status.Conditions = append(updated.Status.Conditions, cond)
	}
	return r.Status().Update(ctx, updated)
}

// SetupWithManager sets up the controller with the Manager.
func (r *FrpProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Index FrpProxy by spec.clientRef.name for efficient listing.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tunnelv1alpha1.FrpProxy{},
		"spec.clientRef.name",
		func(obj client.Object) []string {
			p := obj.(*tunnelv1alpha1.FrpProxy)
			return []string{p.Spec.ClientRef.Name}
		},
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.FrpProxy{}).
		Named("frpproxy").
		Complete(r)
}
