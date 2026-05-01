package config_test

import (
	"strings"
	"testing"

	"tunnel.io/cloud-nexus-operator/internal/config"
)

func TestRenderFrpcToml_BaseConfig(t *testing.T) {
	base := config.BaseParams{
		ServerAddr: "10.0.0.1",
		ServerPort: 7000,
		AuthToken:  "token123",
		AdminPort:  7400,
		AdminToken: "admintoken",
	}
	out, err := config.RenderFrpcToml(base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `serverAddr = "10.0.0.1"`) {
		t.Errorf("expected serverAddr, got:\n%s", out)
	}
	if !strings.Contains(out, "serverPort = 7000") {
		t.Errorf("expected serverPort, got:\n%s", out)
	}
	if !strings.Contains(out, `auth.token = "token123"`) {
		t.Errorf("expected auth.token, got:\n%s", out)
	}
	if !strings.Contains(out, "webServer.port = 7400") {
		t.Errorf("expected webServer.port, got:\n%s", out)
	}
}

func TestRenderFrpcToml_WithTCPProxy(t *testing.T) {
	base := config.BaseParams{ServerAddr: "server.example.com", ServerPort: 7000}
	proxies := []config.ProxyParams{
		{Name: "my-ssh-tunnel", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6000},
	}
	out, err := config.RenderFrpcToml(base, proxies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `[[proxies]]`) {
		t.Errorf("expected proxy block, got:\n%s", out)
	}
	if !strings.Contains(out, `name = "my-ssh-tunnel"`) {
		t.Errorf("expected proxy name, got:\n%s", out)
	}
	if !strings.Contains(out, `type = "tcp"`) {
		t.Errorf("expected type tcp, got:\n%s", out)
	}
	if !strings.Contains(out, "localPort = 22") {
		t.Errorf("expected localPort, got:\n%s", out)
	}
	if !strings.Contains(out, "remotePort = 6000") {
		t.Errorf("expected remotePort, got:\n%s", out)
	}
}

func TestRenderFrpcToml_WithHTTPProxy(t *testing.T) {
	base := config.BaseParams{ServerAddr: "server.example.com", ServerPort: 7000}
	proxies := []config.ProxyParams{
		{Name: "my-http-proxy", Type: "http", LocalIP: "127.0.0.1", LocalPort: 8080,
			CustomDomains: []string{"app.example.com"}},
	}
	out, err := config.RenderFrpcToml(base, proxies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `customDomains = ["app.example.com"]`) {
		t.Errorf("expected customDomains, got:\n%s", out)
	}
}

func TestRenderFrpcToml_WithSTCPProxy(t *testing.T) {
	base := config.BaseParams{ServerAddr: "server.example.com", ServerPort: 7000}
	proxies := []config.ProxyParams{
		{Name: "my-stcp", Type: "stcp", LocalIP: "127.0.0.1", LocalPort: 8080,
			SecretKey: "s3cr3t", AllowUsers: []string{"visitor1"}},
	}
	out, err := config.RenderFrpcToml(base, proxies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `secretKey = "s3cr3t"`) {
		t.Errorf("expected secretKey, got:\n%s", out)
	}
	if !strings.Contains(out, `allowUsers = ["visitor1"]`) {
		t.Errorf("expected allowUsers, got:\n%s", out)
	}
}

func TestRenderFrpcToml_WithTCPMuxProxy(t *testing.T) {
	base := config.BaseParams{ServerAddr: "server.example.com", ServerPort: 7000}
	proxies := []config.ProxyParams{
		{Name: "my-tcpmux", Type: "tcpmux", LocalIP: "127.0.0.1", LocalPort: 8080,
			Multiplexer: "httpconnect", CustomDomains: []string{"mux.example.com"}},
	}
	out, err := config.RenderFrpcToml(base, proxies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `multiplexer = "httpconnect"`) {
		t.Errorf("expected multiplexer, got:\n%s", out)
	}
}

func TestRenderFrpcToml_MultipleArrayElements(t *testing.T) {
	base := config.BaseParams{ServerAddr: "s.example.com", ServerPort: 7000}
	proxies := []config.ProxyParams{
		{Name: "multi-http", Type: "http", LocalIP: "127.0.0.1", LocalPort: 8080,
			CustomDomains: []string{"a.example.com", "b.example.com"}},
		{Name: "multi-stcp", Type: "stcp", LocalIP: "127.0.0.1", LocalPort: 9090,
			SecretKey: "k", AllowUsers: []string{"alice", "bob"}},
	}
	out, err := config.RenderFrpcToml(base, proxies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"a.example.com", "b.example.com"`) {
		t.Errorf("expected two customDomains, got:\n%s", out)
	}
	if !strings.Contains(out, `"alice", "bob"`) {
		t.Errorf("expected two allowUsers, got:\n%s", out)
	}
}
