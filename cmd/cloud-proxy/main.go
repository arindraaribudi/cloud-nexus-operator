// cmd/cloud-proxy/main.go
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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type handler struct {
	stripPrefix    string
	allowedDomains []string

	gcpTS oauth2.TokenSource
	gcpMu sync.Mutex
}

func main() {
	stripPrefix := os.Getenv("STRIP_PREFIX")
	if stripPrefix == "" {
		slog.Error("STRIP_PREFIX env var is required")
		os.Exit(1)
	}
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "8080"
	}
	var allowed []string
	if raw := os.Getenv("ALLOWED_DOMAINS"); raw != "" {
		for _, d := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(d); t != "" {
				allowed = append(allowed, t)
			}
		}
	}

	h := &handler{stripPrefix: stripPrefix, allowedDomains: allowed}
	addr := ":" + port
	slog.Info("cloud-proxy starting", "addr", addr, "prefix", stripPrefix)
	if err := http.ListenAndServe(addr, h); err != nil {
		slog.Error("cloud-proxy exited", "error", err)
		os.Exit(1)
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cloudFQDN, apiPath, err := parseCloudRequest(r.URL.Path, h.stripPrefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !isAllowedDomain(cloudFQDN, h.allowedDomains) {
		http.Error(w, "domain not in allowedDomains", http.StatusForbidden)
		return
	}
	provider := cloudProvider(cloudFQDN)
	if provider == "" {
		http.Error(w, "unsupported cloud domain: "+cloudFQDN, http.StatusBadRequest)
		return
	}

	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusInternalServerError)
			return
		}
	}

	upstreamURL := &url.URL{
		Scheme:   "https",
		Host:     cloudFQDN,
		Path:     apiPath,
		RawQuery: r.URL.RawQuery,
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	for k, vs := range r.Header {
		if strings.EqualFold(k, "host") || strings.EqualFold(k, "authorization") {
			continue
		}
		for _, v := range vs {
			outReq.Header.Add(k, v)
		}
	}
	outReq.Host = cloudFQDN
	outReq.ContentLength = int64(len(bodyBytes))

	switch provider {
	case "aws":
		if err := h.signAWS(r.Context(), outReq, bodyBytes, cloudFQDN); err != nil {
			writeJSONError(w, http.StatusBadGateway, "AWS auth error: "+err.Error())
			return
		}
	case "gcp":
		if err := h.signGCP(r.Context(), outReq); err != nil {
			writeJSONError(w, http.StatusBadGateway, "GCP auth error: "+err.Error())
			return
		}
	case "azure":
		if err := signAzure(r.Context(), outReq, cloudFQDN); err != nil {
			writeJSONError(w, http.StatusBadGateway, "Azure auth error: "+err.Error())
			return
		}
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(outReq)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "upstream error: "+err.Error())
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		slog.Error("copy response body", "error", err)
	}
}

func (h *handler) signAWS(ctx context.Context, req *http.Request, body []byte, fqdn string) error {
	service, region, err := parseAWSService(fqdn)
	if err != nil {
		return err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return fmt.Errorf("AWS config: %w", err)
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("AWS credentials: %w", err)
	}
	hash := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(hash[:])
	signer := v4.NewSigner()
	return signer.SignHTTP(ctx, creds, req, bodyHash, service, region, time.Now())
}

func (h *handler) signGCP(ctx context.Context, req *http.Request) error {
	h.gcpMu.Lock()
	defer h.gcpMu.Unlock()
	if h.gcpTS == nil {
		ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return fmt.Errorf("GCP token source: %w", err)
		}
		h.gcpTS = ts
	}
	token, err := h.gcpTS.Token()
	if err != nil {
		return fmt.Errorf("GCP token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	return nil
}

func signAzure(ctx context.Context, req *http.Request, cloudFQDN string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("Azure credential: %w", err)
	}
	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://" + cloudFQDN + "/.default"},
	})
	if err != nil {
		return fmt.Errorf("Azure token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	return nil
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// parseCloudRequest strips the prefix from path and returns the cloud FQDN and API path.
// path:   /spoke1/cloud/secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp
// prefix: /spoke1/cloud
// →       fqdn=secretsmanager.us-east-1.amazonaws.com  apiPath=/v1/secret/myapp
func parseCloudRequest(path, prefix string) (cloudFQDN, apiPath string, err error) {
	if !strings.HasPrefix(path, prefix) {
		return "", "", fmt.Errorf("path %q does not start with prefix %q", path, prefix)
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(path, prefix), "/")
	if rest == "" {
		return "", "", fmt.Errorf("empty path after prefix")
	}
	slash := strings.Index(rest, "/")
	if slash == -1 {
		return rest, "/", nil
	}
	return rest[:slash], rest[slash:], nil
}

// cloudProvider returns "aws", "gcp", "azure", or "" for unrecognised domains.
func cloudProvider(fqdn string) string {
	switch {
	case strings.HasSuffix(fqdn, ".amazonaws.com"):
		return "aws"
	case strings.HasSuffix(fqdn, ".googleapis.com"):
		return "gcp"
	case strings.HasSuffix(fqdn, ".azure.net"),
		strings.HasSuffix(fqdn, ".azure.com"),
		strings.HasSuffix(fqdn, ".azconfig.io"):
		return "azure"
	default:
		return ""
	}
}

// isAllowedDomain reports whether fqdn matches any pattern in patterns.
// An empty patterns slice allows all domains.
func isAllowedDomain(fqdn string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if matchDomain(fqdn, p) {
			return true
		}
	}
	return false
}

// matchDomain matches fqdn against pattern, supporting leading "*." wildcards.
func matchDomain(fqdn, pattern string) bool {
	if pattern == fqdn {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(fqdn, pattern[1:])
	}
	return false
}

// parseAWSService extracts the service name and region from an AWS FQDN.
// e.g. secretsmanager.us-east-1.amazonaws.com → "secretsmanager", "us-east-1"
func parseAWSService(fqdn string) (service, region string, err error) {
	trimmed := strings.TrimSuffix(fqdn, ".amazonaws.com")
	parts := strings.SplitN(trimmed, ".", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", fmt.Errorf("unexpected AWS FQDN format: %s", fqdn)
	}
	return parts[0], parts[1], nil
}
