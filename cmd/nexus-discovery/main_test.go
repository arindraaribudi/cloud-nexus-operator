package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeNexusInfo(t *testing.T) {
	info := nexusInfo{
		GatewayFQDN: "hub-gateway.default.svc.cluster.local",
		GatewayPort: 8080,
		ServerName:  "hub",
		Namespace:   "default",
	}
	h := newHandler(info)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/nexus-info")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("want application/json, got %q", ct)
	}

	var got nexusInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.GatewayFQDN != info.GatewayFQDN {
		t.Errorf("GatewayFQDN: want %q, got %q", info.GatewayFQDN, got.GatewayFQDN)
	}
	if got.GatewayPort != info.GatewayPort {
		t.Errorf("GatewayPort: want %d, got %d", info.GatewayPort, got.GatewayPort)
	}
	if got.ServerName != info.ServerName {
		t.Errorf("ServerName: want %q, got %q", info.ServerName, got.ServerName)
	}
	if got.Namespace != info.Namespace {
		t.Errorf("Namespace: want %q, got %q", info.Namespace, got.Namespace)
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	h := newHandler(nexusInfo{})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}
