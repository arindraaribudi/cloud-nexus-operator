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

// Package webhook provides the frps httpPlugin event handler.
package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// frps httpPlugin request/response types (frp plugin protocol v0.1.0).

type pluginRequest struct {
	Version string        `json:"version"`
	Op      string        `json:"op"`
	Content pluginContent `json:"content"`
}

type pluginContent struct {
	ProxyName string            `json:"proxy_name"`
	ProxyType string            `json:"proxy_type"`
	Metas     map[string]string `json:"metas"`
}

type pluginResponse struct {
	Reject       bool   `json:"reject"`
	RejectReason string `json:"reject_reason,omitempty"`
	Unchange     bool   `json:"unchange"`
}

// FrpsEventHandler handles POST /frps/events from frps httpPlugin callbacks.
type FrpsEventHandler struct {
	client   client.Client
	recorder record.EventRecorder
}

// NewFrpsEventHandler constructs a handler using the given controller-runtime client.
// recorder may be nil; if non-nil, Warning events are emitted to Kubernetes on timeout.
func NewFrpsEventHandler(c client.Client, recorder record.EventRecorder) *FrpsEventHandler {
	return &FrpsEventHandler{client: c, recorder: recorder}
}

// Client exposes the underlying client for testing.
func (h *FrpsEventHandler) Client() client.Client { return h.client }

func (h *FrpsEventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := logf.FromContext(r.Context())

	var req pluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "failed to decode frps plugin request, allowing proxy (failSafe)")
		writeAllow(w)
		return
	}

	log.Info("frps plugin event", "op", req.Op, "proxy", req.Content.ProxyName, "svcName", req.Content.Metas["svcName"])

	switch req.Op {
	case "NewProxy":
		h.handleNewProxy(r.Context(), req.Content, w)
	case "CloseProxy":
		h.handleCloseProxy(r.Context(), req.Content, w)
	default:
		writeAllow(w)
	}
}

func (h *FrpsEventHandler) handleNewProxy(ctx context.Context, content pluginContent, w http.ResponseWriter) {
	log := logf.FromContext(ctx)

	svcName := content.Metas["svcName"]
	if svcName == "" {
		writeAllow(w)
		return
	}

	namespace := content.Metas["namespace"]
	frpServerName := content.Metas["frpServerName"]
	remotePortStr := content.Metas["remotePort"]
	remotePort, err := strconv.ParseInt(remotePortStr, 10, 32)
	if err != nil {
		log.Error(err, "invalid remotePort in metas", "value", remotePortStr)
		writeAllow(w) // failSafe: allow proxy even if we can't create the Service
		return
	}

	// Guard against a slow k8s API or unsynced informer cache blocking the frps plugin
	// call indefinitely. frp v0.68.1 has no HTTP client timeout on plugin calls, so a
	// blocked handler stalls the frps session read loop and prevents ALL subsequent
	// proxy registrations.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
		},
	}

	log.Info("CreateOrUpdate start", "svc", svcName, "namespace", namespace)
	start := time.Now()
	op, err := controllerutil.CreateOrUpdate(ctx, h.client, svc, func() error {
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"frpserver": frpServerName}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "registry",
				Port:       80,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(int32(remotePort)),
			},
		}
		return nil
	})
	log.Info("CreateOrUpdate done", "svc", svcName, "namespace", namespace, "elapsed", time.Since(start), "op", op, "err", err)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Error(err, "CreateOrUpdate timed out — k8s API slow or informer cache not synced; check RBAC (watch verb on services)",
				"svc", svcName, "namespace", namespace)
			h.emitTimeoutEvent(namespace, svcName)
		} else {
			log.Error(err, "failed to create hub Service", "name", svcName, "namespace", namespace)
		}
		writeAllow(w) // failSafe
		return
	}

	if op == controllerutil.OperationResultCreated {
		log.Info("created hub registry Service", "name", svcName, "namespace", namespace)
	}
	writeAllow(w)
}

// emitTimeoutEvent emits a Kubernetes Warning event on the target namespace when a
// CreateOrUpdate call times out, making the problem visible in kubectl describe/events
// without requiring direct log access.
func (h *FrpsEventHandler) emitTimeoutEvent(namespace, svcName string) {
	if h.recorder == nil {
		return
	}
	// Use a background context — the request context is already expired.
	fetchCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second) //nolint:contextcheck
	defer cancel()
	var ns corev1.Namespace
	if err := h.client.Get(fetchCtx, client.ObjectKey{Name: namespace}, &ns); err != nil {
		// If we can't fetch the namespace, skip the event; the log.Error above still fires.
		return
	}
	h.recorder.Eventf(&ns, corev1.EventTypeWarning, "WebhookTimeout",
		"frps-webhook timed out creating/updating Service %s/%s; verify RBAC watch permission on services", namespace, svcName)
}

func (h *FrpsEventHandler) handleCloseProxy(ctx context.Context, content pluginContent, w http.ResponseWriter) {
	log := logf.FromContext(ctx)

	svcName := content.Metas["svcName"]
	if svcName == "" {
		writeAllow(w)
		return
	}

	namespace := content.Metas["namespace"]
	svc := &corev1.Service{}
	if err := h.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: svcName}, svc); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get hub Service for deletion", "name", svcName)
		}
		writeAllow(w)
		return
	}

	if err := h.client.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "failed to delete hub Service", "name", svcName)
	} else {
		log.Info("deleted hub registry Service", "name", svcName, "namespace", namespace)
	}
	writeAllow(w)
}

func writeAllow(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	// Unchange: true preserves the original proxy config from frpc.
	// frp v0.68.1 plugin manager: Unchange=false replaces the proxy config with
	// the response Content field; since we return no Content, that would register
	// a zero-value proxy (name="") instead of the intended named proxy.
	_ = json.NewEncoder(w).Encode(pluginResponse{Reject: false, Unchange: true})
}

// HTTPServer wraps FrpsEventHandler as a controller-runtime Runnable.
type HTTPServer struct {
	addr    string
	handler *FrpsEventHandler
}

// NewHTTPServer returns an HTTPServer that implements manager.Runnable.
// recorder may be nil; if non-nil, Warning events are emitted to Kubernetes on timeout.
func NewHTTPServer(addr string, c client.Client, recorder record.EventRecorder) *HTTPServer {
	return &HTTPServer{addr: addr, handler: NewFrpsEventHandler(c, recorder)}
}

// Start implements manager.Runnable. Blocks until ctx is cancelled and server shuts down.
func (s *HTTPServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/frps/events", s.handler)
	srv := &http.Server{Addr: s.addr, Handler: mux}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logf.Log.Error(err, "frps-plugin HTTP server shutdown error")
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	<-shutdownDone
	return nil
}
