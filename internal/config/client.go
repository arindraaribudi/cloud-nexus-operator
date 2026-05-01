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
)

// BaseParams holds the frpc base configuration for TOML rendering.
type BaseParams struct {
	ServerAddr  string
	ServerPort  int32
	AuthToken   string
	AdminPort   int32
	AdminToken  string
	ExtraConfig string
}

// ProxyParams holds all per-proxy fields for TOML rendering.
// Each typed proxy CRD populates only the fields relevant to its type.
type ProxyParams struct {
	Name          string
	Type          string
	LocalIP       string
	LocalPort     int32
	RemotePort    int32
	CustomDomains []string
	SecretKey     string
	Subdomain     string
	AllowUsers    []string
	Multiplexer   string
	ExtraConfig   string
}

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
{{- if .Subdomain }}
subdomain = "{{ .Subdomain }}"
{{- end }}
{{- if .SecretKey }}
secretKey = "{{ .SecretKey }}"
{{- end }}
{{- if .AllowUsers }}
allowUsers = [{{ range $i, $u := .AllowUsers }}{{if $i}}, {{end}}"{{ $u }}"{{end}}]
{{- end }}
{{- if .Multiplexer }}
multiplexer = "{{ .Multiplexer }}"
{{- end }}
{{ .ExtraConfig }}
`))

// RenderFrpcToml renders the full frpc.toml from base config and a list of proxy params.
func RenderFrpcToml(base BaseParams, proxies []ProxyParams) (string, error) {
	var buf bytes.Buffer
	if err := frpcBaseTemplate.Execute(&buf, base); err != nil {
		return "", fmt.Errorf("render frpc base: %w", err)
	}
	for _, p := range proxies {
		if p.LocalIP == "" {
			p.LocalIP = "127.0.0.1"
		}
		if err := frpcProxyTemplate.Execute(&buf, p); err != nil {
			return "", fmt.Errorf("render proxy block %q: %w", p.Name, err)
		}
	}
	return buf.String(), nil
}
