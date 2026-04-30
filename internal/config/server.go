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

// Package config renders TOML configuration files for frps and frpc.
package config

import (
	"bytes"
	"fmt"
	"text/template"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

var frpsTemplate = template.Must(template.New("frps").Parse(`bindPort = {{ .BindPort }}
{{- if .VhostHTTPPort }}
vhostHTTPPort = {{ .VhostHTTPPort }}
{{- end }}
{{- if .VhostHTTPSPort }}
vhostHTTPSPort = {{ .VhostHTTPSPort }}
{{- end }}
{{- if .DashboardPort }}
webServer.port = {{ .DashboardPort }}
{{- end }}
{{- if .DashboardUser }}
webServer.user = "{{ .DashboardUser }}"
{{- end }}
{{- if .DashboardPassword }}
webServer.password = "{{ .DashboardPassword }}"
{{- end }}
{{- if .AuthToken }}
auth.method = "token"
auth.token = "{{ .AuthToken }}"
{{- end }}
{{- if .ExtraConfig }}
{{ .ExtraConfig }}
{{- end }}
{{- if .EnableOperatorPlugin }}

[[httpPlugins]]
name     = "operator-notifier"
addr     = "{{ .OperatorWebhookAddr }}"
path     = "/frps/events"
ops      = ["NewProxy", "CloseProxy"]
{{- end }}
`))

type frpsData struct {
	BindPort             int32
	VhostHTTPPort        int32
	VhostHTTPSPort       int32
	DashboardPort        int32
	DashboardUser        string
	DashboardPassword    string
	AuthToken            string
	ExtraConfig          string
	EnableOperatorPlugin bool
	OperatorWebhookAddr  string
}

// RenderFrpsToml renders the frps.toml content from the given spec and resolved secrets.
// defaultWebhookAddr is used as the [[httpPlugins]] addr when spec.OperatorWebhookAddr is empty.
// The caller is responsible for constructing the correct service FQDN.
func RenderFrpsToml(spec tunnelv1alpha1.NexusServerSpec, authToken, dashPassword, defaultWebhookAddr string) (string, error) {
	webhookAddr := spec.OperatorWebhookAddr
	if webhookAddr == "" && spec.EnableOperatorPlugin {
		webhookAddr = defaultWebhookAddr
	}

	data := frpsData{
		BindPort:             spec.BindPort,
		VhostHTTPPort:        spec.VhostHTTPPort,
		VhostHTTPSPort:       spec.VhostHTTPSPort,
		DashboardPort:        spec.DashboardPort,
		DashboardUser:        spec.DashboardUser,
		DashboardPassword:    dashPassword,
		AuthToken:            authToken,
		ExtraConfig:          spec.ExtraConfig,
		EnableOperatorPlugin: spec.EnableOperatorPlugin,
		OperatorWebhookAddr:  webhookAddr,
	}

	if data.BindPort == 0 {
		data.BindPort = 7000
	}

	var buf bytes.Buffer
	if err := frpsTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render frps.toml: %w", err)
	}
	return buf.String(), nil
}
