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

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type handler struct {
	// gcpTS is the GCP token source (long-lived, caches & refreshes internally).
	// Initialised lazily on first GCP request.
	gcpTS oauth2.TokenSource
	gcpMu sync.Mutex

	// ecrToken caches the ECR Basic auth header value.
	ecrToken  string
	ecrExpiry time.Time
	ecrMu     sync.Mutex
}

func main() {
	port := flag.Int("port", 5000, "port the registry proxy listens on")
	flag.Parse()

	h := &handler{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", h.handleRequest)
	addr := fmt.Sprintf(":%d", *port)
	slog.Info("registry-proxy starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("registry-proxy exited", "error", err)
	}
}

func (h *handler) handleRequest(w http.ResponseWriter, r *http.Request) {
	// /v2/ ping — return 200 so Docker clients don't hit the auth challenge.
	if r.URL.Path == "/v2/" || r.URL.Path == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}

	upstreamHost, imagePath, ok := parseUpstream(r.URL.Path)
	if !ok {
		http.Error(w, "registry-proxy: cannot parse upstream host from path", http.StatusBadRequest)
		return
	}

	authHeader, err := h.buildAuthHeader(r.Context(), upstreamHost)
	if err != nil {
		slog.Error("auth failed", "upstream", upstreamHost, "error", err)
		http.Error(w, "registry-proxy: auth error", http.StatusBadGateway)
		return
	}

	target := &url.URL{
		Scheme:   "https",
		Host:     upstreamHost,
		Path:     "/v2/" + imagePath,
		RawQuery: r.URL.RawQuery,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL = target
			req.Host = upstreamHost
			req.Header.Del("Authorization")
			if authHeader != "" {
				req.Header.Set("Authorization", authHeader)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// AR blob downloads return a relative redirect (e.g. /artifacts-downloads/...).
			// The download endpoint requires no auth (the path token is short-lived).
			// Rewrite the Location to an absolute URL so Docker follows it directly to
			// the upstream registry rather than looping back through this proxy.
			if resp.StatusCode/100 == 3 {
				loc := resp.Header.Get("Location")
				if loc != "" && !strings.HasPrefix(loc, "http") {
					resp.Header.Set("Location", "https://"+upstreamHost+loc)
				}
			}
			return nil
		},
	}
	proxy.ServeHTTP(w, r)
}

// parseUpstream extracts the upstream registry hostname from a Docker registry
// API path. The first segment after /v2/ that contains a "." is treated as the
// upstream host.
//
// /v2/asia-southeast3-docker.pkg.dev/project/image/manifests/latest
//
//	→ host="asia-southeast3-docker.pkg.dev", rest="project/image/manifests/latest"
func parseUpstream(path string) (host, rest string, ok bool) {
	trimmed := strings.TrimPrefix(path, "/v2/")
	if trimmed == "" || trimmed == path {
		return "", "", false
	}
	idx := strings.Index(trimmed, "/")
	if idx < 0 {
		return "", "", false
	}
	candidate := trimmed[:idx]
	if !strings.Contains(candidate, ".") {
		return "", "", false
	}
	return candidate, trimmed[idx+1:], true
}

// authScheme returns "gcp", "ecr", or "" depending on the upstream host.
func authScheme(host string) string {
	if host == "gcr.io" || strings.HasSuffix(host, ".gcr.io") || strings.HasSuffix(host, ".pkg.dev") {
		return "gcp"
	}
	if strings.Contains(host, ".dkr.ecr.") && strings.HasSuffix(host, ".amazonaws.com") {
		return "ecr"
	}
	return ""
}

func (h *handler) buildAuthHeader(ctx context.Context, upstream string) (string, error) {
	switch authScheme(upstream) {
	case "gcp":
		return h.gcpBearerHeader(ctx)
	case "ecr":
		return h.ecrBasicHeader(ctx, upstream)
	default:
		return "", nil
	}
}

func (h *handler) gcpBearerHeader(ctx context.Context) (string, error) {
	h.gcpMu.Lock()
	defer h.gcpMu.Unlock()
	if h.gcpTS == nil {
		ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return "", fmt.Errorf("GCP ADC token source: %w", err)
		}
		h.gcpTS = ts
	}
	token, err := h.gcpTS.Token()
	if err != nil {
		return "", fmt.Errorf("GCP token: %w", err)
	}
	return "Bearer " + token.AccessToken, nil
}

func (h *handler) ecrBasicHeader(ctx context.Context, host string) (string, error) {
	h.ecrMu.Lock()
	defer h.ecrMu.Unlock()
	// Refresh if expired or within 5 minutes of expiry.
	if h.ecrToken == "" || time.Until(h.ecrExpiry) < 5*time.Minute {
		parts := strings.Split(host, ".")
		if len(parts) < 6 {
			return "", fmt.Errorf("unexpected ECR host format: %s", host)
		}
		region := parts[3]
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return "", fmt.Errorf("AWS config: %w", err)
		}
		out, err := ecr.NewFromConfig(cfg).GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
		if err != nil {
			return "", fmt.Errorf("ECR GetAuthorizationToken: %w", err)
		}
		if len(out.AuthorizationData) == 0 || out.AuthorizationData[0].AuthorizationToken == nil {
			return "", fmt.Errorf("ECR returned no authorization data")
		}
		expiry := time.Now().Add(12 * time.Hour)
		if out.AuthorizationData[0].ExpiresAt != nil {
			expiry = *out.AuthorizationData[0].ExpiresAt
		}
		h.ecrToken = "Basic " + *out.AuthorizationData[0].AuthorizationToken
		h.ecrExpiry = expiry
	}
	return h.ecrToken, nil
}
