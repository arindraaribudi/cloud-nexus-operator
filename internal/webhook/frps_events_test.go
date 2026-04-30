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

package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"tunnel.io/cloud-nexus-operator/internal/webhook"
)

func newHandler(t *testing.T) *webhook.FrpsEventHandler {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	return webhook.NewFrpsEventHandler(c, nil)
}

func postEvent(t *testing.T, h *webhook.FrpsEventHandler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/frps/events", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestFrpsEventHandler_NewProxy_CreatesService(t *testing.T) {
	h := newHandler(t)
	rr := postEvent(t, h, map[string]any{
		"version": "0.1.0",
		"op":      "NewProxy",
		"content": map[string]any{
			"proxy_name":  "reg-1-registry-proxy",
			"proxy_type":  "tcp",
			"remote_port": 15000,
			"metas": map[string]string{
				"svcName":       "reg-1-docker",
				"namespace":     "tunnels",
				"remotePort":    "15000",
				"frpServerName": "main-hub",
			},
		},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if reject, _ := resp["reject"].(bool); reject {
		t.Errorf("expected reject=false")
	}

	// Verify the Service was created via the handler's embedded client.
	var svc corev1.Service
	if err := h.Client().Get(context.Background(), types.NamespacedName{
		Namespace: "tunnels",
		Name:      "reg-1-docker",
	}, &svc); err != nil {
		t.Fatalf("Service not created: %v", err)
	}
	if svc.Spec.Selector["frpserver"] != "main-hub" {
		t.Errorf("expected selector frpserver=main-hub, got %v", svc.Spec.Selector)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 80 {
		t.Errorf("expected port 80, got %v", svc.Spec.Ports)
	}
	if int(svc.Spec.Ports[0].TargetPort.IntVal) != 15000 {
		t.Errorf("expected targetPort 15000, got %v", svc.Spec.Ports[0].TargetPort)
	}
}

func TestFrpsEventHandler_CloseProxy_DeletesService(t *testing.T) {
	h := newHandler(t)

	// Pre-create a Service.
	existing := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reg-1-docker",
			Namespace: "tunnels",
		},
	}
	if err := h.Client().Create(context.Background(), existing); err != nil {
		t.Fatalf("pre-create service: %v", err)
	}

	rr := postEvent(t, h, map[string]any{
		"version": "0.1.0",
		"op":      "CloseProxy",
		"content": map[string]any{
			"proxy_name": "reg-1-registry-proxy",
			"metas": map[string]string{
				"svcName":   "reg-1-docker",
				"namespace": "tunnels",
			},
		},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var svc corev1.Service
	if err := h.Client().Get(context.Background(), types.NamespacedName{
		Namespace: "tunnels",
		Name:      "reg-1-docker",
	}, &svc); err == nil {
		t.Error("expected Service to be deleted but it still exists")
	}
}

func TestFrpsEventHandler_NoSvcName_Passthrough(t *testing.T) {
	h := newHandler(t)
	rr := postEvent(t, h, map[string]any{
		"version": "0.1.0",
		"op":      "NewProxy",
		"content": map[string]any{
			"proxy_name": "some-other-proxy",
			"metas":      map[string]string{},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if reject, _ := resp["reject"].(bool); reject {
		t.Errorf("expected reject=false for non-registry proxy")
	}
}
