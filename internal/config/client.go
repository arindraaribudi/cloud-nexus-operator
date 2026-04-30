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

package config

import (
	"bytes"
	"fmt"
	"text/template"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

var frpcBaseTemplate = template.Must(template.New("frpc-base").Parse(`serverAddr = "{{ .ServerAddr }}"
serverPort = {{ .ServerPort }}
{{- if .AuthToken }}
auth.method = "token"
auth.token = "{{ .AuthToken }}"
{{- end }}
{{- if .AdminPort }}
webServer.port = {{ .AdminPort }}
webServer.addr = "0.0.0.0"
{{- if .AdminToken }}
webServer.password = "{{ .AdminToken }}"
{{- end }}
{{- end }}
{{ .ExtraConfig }}
`))

var frpcProxyTemplate = template.Must(template.New("frpc-proxy").Parse(`
[[proxies]]
name = "{{ .Name }}"
type = "{{ .Type }}"
{{- if .LocalIP }}
localIP = "{{ .LocalIP }}"
{{- end }}
localPort = {{ .LocalPort }}
{{- if .RemotePort }}
remotePort = {{ .RemotePort }}
{{- end }}
{{- if .CustomDomains }}
customDomains = [{{ range $i, $d := .CustomDomains }}{{if $i}}, {{end}}"{{ $d }}"{{end}}]
{{- end }}
{{- if .SecretKey }}
secretKey = "{{ .SecretKey }}"
{{- end }}
{{ .ExtraConfig }}
`))

type frpcBaseData struct {
	ServerAddr  string
	ServerPort  int32
	AuthToken   string
	AdminPort   int32
	AdminToken  string
	ExtraConfig string
}

type frpcProxyData struct {
	Name          string
	Type          string
	LocalIP       string
	LocalPort     int32
	RemotePort    int32
	CustomDomains []string
	SecretKey     string
	ExtraConfig   string
}

// RenderFrpcToml renders the full frpc.toml from the client spec and a list of proxies.
// serverAddr and serverPort are the resolved server coordinates.
// authToken and adminToken are the resolved secret values.
func RenderFrpcToml(
	spec tunnelv1alpha1.NexusClientSpec,
	serverAddr string, serverPort int32,
	authToken, adminToken string,
	proxies []tunnelv1alpha1.FrpProxy,
) (string, error) {
	adminPort := spec.Admin.Port
	if adminPort == 0 {
		adminPort = 7400
	}

	base := frpcBaseData{
		ServerAddr:  serverAddr,
		ServerPort:  serverPort,
		AuthToken:   authToken,
		AdminPort:   adminPort,
		AdminToken:  adminToken,
		ExtraConfig: spec.ExtraConfig,
	}

	var buf bytes.Buffer
	if err := frpcBaseTemplate.Execute(&buf, base); err != nil {
		return "", fmt.Errorf("render frpc base: %w", err)
	}

	for _, p := range proxies {
		pdata := frpcProxyData{
			Name:          p.Name,
			Type:          p.Spec.Type,
			LocalIP:       p.Spec.LocalIP,
			LocalPort:     p.Spec.LocalPort,
			RemotePort:    p.Spec.RemotePort,
			CustomDomains: p.Spec.CustomDomains,
			SecretKey:     p.Spec.SecretKey,
			ExtraConfig:   p.Spec.ExtraConfig,
		}
		if pdata.LocalIP == "" {
			pdata.LocalIP = "127.0.0.1"
		}
		if err := frpcProxyTemplate.Execute(&buf, pdata); err != nil {
			return "", fmt.Errorf("render proxy block: %w", err)
		}
	}

	return buf.String(), nil
}
