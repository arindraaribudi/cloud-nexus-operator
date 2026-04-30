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

package config_test

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
	"tunnel.io/cloud-nexus-operator/internal/config"
)

func TestRenderFrpcToml_BaseConfig(t *testing.T) {
	spec := tunnelv1alpha1.NexusClientSpec{
		Admin: tunnelv1alpha1.AdminSpec{Port: 7400},
	}
	out, err := config.RenderFrpcToml(spec, "10.0.0.1", 7000, "token123", "admintoken", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `serverAddr = "10.0.0.1"`) {
		t.Errorf("expected serverAddr in output, got:\n%s", out)
	}
	if !strings.Contains(out, "serverPort = 7000") {
		t.Errorf("expected serverPort in output, got:\n%s", out)
	}
	if !strings.Contains(out, `auth.token = "token123"`) {
		t.Errorf("expected auth.token in output, got:\n%s", out)
	}
	if !strings.Contains(out, "webServer.port = 7400") {
		t.Errorf("expected webServer.port in output, got:\n%s", out)
	}
}

func TestRenderFrpcToml_WithTCPProxy(t *testing.T) {
	spec := tunnelv1alpha1.NexusClientSpec{}
	proxies := []tunnelv1alpha1.FrpProxy{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ssh-tunnel"},
			Spec: tunnelv1alpha1.FrpProxySpec{
				ClientRef:  tunnelv1alpha1.LocalObjectRef{Name: "spoke-1"},
				Type:       "tcp",
				LocalPort:  22,
				RemotePort: 6000,
			},
		},
	}
	out, err := config.RenderFrpcToml(spec, "server.example.com", 7000, "", "", proxies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `[[proxies]]`) {
		t.Errorf("expected proxy block in output, got:\n%s", out)
	}
	if !strings.Contains(out, `name = "my-ssh-tunnel"`) {
		t.Errorf("expected proxy name in output, got:\n%s", out)
	}
	if !strings.Contains(out, `type = "tcp"`) {
		t.Errorf("expected type tcp in output, got:\n%s", out)
	}
	if !strings.Contains(out, "localPort = 22") {
		t.Errorf("expected localPort in output, got:\n%s", out)
	}
	if !strings.Contains(out, "remotePort = 6000") {
		t.Errorf("expected remotePort in output, got:\n%s", out)
	}
}

func TestRenderFrpcToml_WithHTTPProxy(t *testing.T) {
	spec := tunnelv1alpha1.NexusClientSpec{}
	proxies := []tunnelv1alpha1.FrpProxy{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "my-http-proxy"},
			Spec: tunnelv1alpha1.FrpProxySpec{
				ClientRef:     tunnelv1alpha1.LocalObjectRef{Name: "spoke-1"},
				Type:          "http",
				LocalPort:     8080,
				CustomDomains: []string{"app.example.com"},
			},
		},
	}
	out, err := config.RenderFrpcToml(spec, "server.example.com", 7000, "", "", proxies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `customDomains = ["app.example.com"]`) {
		t.Errorf("expected customDomains in output, got:\n%s", out)
	}
}
