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

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
	"tunnel.io/cloud-nexus-operator/internal/config"
)

func TestRenderFrpsToml_BasicPort(t *testing.T) {
	spec := tunnelv1alpha1.NexusServerSpec{
		BindPort: 7000,
	}
	out, err := config.RenderFrpsToml(spec, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "bindPort = 7000") {
		t.Errorf("expected bindPort in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_AuthToken(t *testing.T) {
	spec := tunnelv1alpha1.NexusServerSpec{BindPort: 7000}
	out, err := config.RenderFrpsToml(spec, "mysecret", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `auth.token = "mysecret"`) {
		t.Errorf("expected auth.token in output, got:\n%s", out)
	}
	if !strings.Contains(out, `auth.method = "token"`) {
		t.Errorf("expected auth.method in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_Dashboard(t *testing.T) {
	spec := tunnelv1alpha1.NexusServerSpec{
		BindPort:      7000,
		DashboardPort: 7500,
		DashboardUser: "admin",
	}
	out, err := config.RenderFrpsToml(spec, "", "pass123", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "webServer.port = 7500") {
		t.Errorf("expected webServer.port in output, got:\n%s", out)
	}
	if !strings.Contains(out, `webServer.user = "admin"`) {
		t.Errorf("expected webServer.user in output, got:\n%s", out)
	}
	if !strings.Contains(out, `webServer.password = "pass123"`) {
		t.Errorf("expected webServer.password in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_ExtraConfig(t *testing.T) {
	spec := tunnelv1alpha1.NexusServerSpec{
		BindPort:    7000,
		ExtraConfig: "maxPoolCount = 5",
	}
	out, err := config.RenderFrpsToml(spec, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "maxPoolCount = 5") {
		t.Errorf("expected extraConfig in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_OperatorPlugin(t *testing.T) {
	spec := tunnelv1alpha1.NexusServerSpec{
		BindPort:             7000,
		EnableOperatorPlugin: true,
	}
	out, err := config.RenderFrpsToml(spec, "", "", "controller-manager-frps-plugin.mynamespace.svc.cluster.local:9090")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `[[httpPlugins]]`) {
		t.Errorf("expected [[httpPlugins]] in output, got:\n%s", out)
	}
	if !strings.Contains(out, `controller-manager-frps-plugin.mynamespace.svc.cluster.local:9090`) {
		t.Errorf("expected webhook addr in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_OperatorPluginCustomAddr(t *testing.T) {
	spec := tunnelv1alpha1.NexusServerSpec{
		BindPort:             7000,
		EnableOperatorPlugin: true,
		OperatorWebhookAddr:  "my-custom-svc.ns.svc.cluster.local:9090",
	}
	out, err := config.RenderFrpsToml(spec, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `my-custom-svc.ns.svc.cluster.local:9090`) {
		t.Errorf("expected custom addr in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_NoPlugin(t *testing.T) {
	spec := tunnelv1alpha1.NexusServerSpec{BindPort: 7000}
	out, err := config.RenderFrpsToml(spec, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, `[[httpPlugins]]`) {
		t.Errorf("expected no httpPlugins when disabled, got:\n%s", out)
	}
}
