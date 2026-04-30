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

package reload_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tunnel.io/cloud-nexus-operator/internal/reload"
)

func TestReload_Success(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Parse host and port from test server URL
	addr := strings.TrimPrefix(srv.URL, "http://")
	var host string
	var port int32
	if _, err := fmt.Sscanf(addr, "%s", &addr); err == nil {
		parts := strings.Split(addr, ":")
		host = parts[0]
		_, _ = fmt.Sscanf(parts[1], "%d", &port)
	}

	c := reload.New(srv.Client())
	if err := c.Reload(context.Background(), host, port, "mytoken"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/api/reload" {
		t.Errorf("expected /api/reload, got %s", gotPath)
	}
	// Basic Auth: base64("":mytoken)
	if gotAuth == "" {
		t.Errorf("expected Authorization header, got empty")
	}
}

func TestReload_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	_, _ = fmt.Sscanf(parts[1], "%d", &port)

	c := reload.New(srv.Client())
	err := c.Reload(context.Background(), host, port, "")
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}
