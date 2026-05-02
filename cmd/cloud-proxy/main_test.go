package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseCloudRequest(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		prefix      string
		wantFQDN    string
		wantAPIPath string
		wantErr     bool
	}{
		{
			name:        "AWS Secrets Manager",
			path:        "/spoke1/cloud/secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp",
			prefix:      "/spoke1/cloud",
			wantFQDN:    "secretsmanager.us-east-1.amazonaws.com",
			wantAPIPath: "/v1/secret/myapp",
		},
		{
			name:        "GCP Secret Manager",
			path:        "/spoke1/cloud/secretmanager.googleapis.com/v1/projects/p/secrets/s/versions/latest:access",
			prefix:      "/spoke1/cloud",
			wantFQDN:    "secretmanager.googleapis.com",
			wantAPIPath: "/v1/projects/p/secrets/s/versions/latest:access",
		},
		{
			name:        "Azure Key Vault",
			path:        "/spoke2/cloud/myvault.vault.azure.net/secrets/myapp",
			prefix:      "/spoke2/cloud",
			wantFQDN:    "myvault.vault.azure.net",
			wantAPIPath: "/secrets/myapp",
		},
		{
			name:    "wrong prefix",
			path:    "/spoke1/registry/something",
			prefix:  "/spoke1/cloud",
			wantErr: true,
		},
		{
			name:    "no FQDN",
			path:    "/spoke1/cloud/",
			prefix:  "/spoke1/cloud",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fqdn, apiPath, err := parseCloudRequest(tc.path, tc.prefix)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fqdn != tc.wantFQDN {
				t.Errorf("FQDN: got %q, want %q", fqdn, tc.wantFQDN)
			}
			if apiPath != tc.wantAPIPath {
				t.Errorf("apiPath: got %q, want %q", apiPath, tc.wantAPIPath)
			}
		})
	}
}

func TestCloudProvider(t *testing.T) {
	tests := []struct {
		fqdn string
		want string
	}{
		{"secretsmanager.us-east-1.amazonaws.com", "aws"},
		{"ssm.eu-west-1.amazonaws.com", "aws"},
		{"secretmanager.googleapis.com", "gcp"},
		{"storage.googleapis.com", "gcp"},
		{"myvault.vault.azure.net", "azure"},
		{"mystore.azconfig.io", "azure"},
		{"management.azure.com", "azure"},
		{"cvm.tencentcloudapi.com", "tencent"},
		{"cbs.tencentcloudapi.com", "tencent"},
		{"cos.ap-beijing.myqcloud.com", "tencent"},
		{"example.com", ""},
		{"github.com", ""},
	}
	for _, tc := range tests {
		got := cloudProvider(tc.fqdn)
		if got != tc.want {
			t.Errorf("cloudProvider(%q) = %q, want %q", tc.fqdn, got, tc.want)
		}
	}
}

func TestIsAllowedDomain(t *testing.T) {
	tests := []struct {
		fqdn     string
		patterns []string
		want     bool
	}{
		{"secretsmanager.us-east-1.amazonaws.com", []string{"*.amazonaws.com"}, true},
		{"secretmanager.googleapis.com", []string{"*.amazonaws.com"}, false},
		{"secretmanager.googleapis.com", []string{"*.amazonaws.com", "*.googleapis.com"}, true},
		{"example.com", []string{}, true}, // empty = allow all
		{"myvault.vault.azure.net", []string{"*.vault.azure.net"}, true},
		{"myvault.vault.azure.net", []string{"*.azure.net"}, true},
	}
	for _, tc := range tests {
		got := isAllowedDomain(tc.fqdn, tc.patterns)
		if got != tc.want {
			t.Errorf("isAllowedDomain(%q, %v) = %v, want %v", tc.fqdn, tc.patterns, got, tc.want)
		}
	}
}

func TestParseAWSService(t *testing.T) {
	tests := []struct {
		fqdn        string
		wantService string
		wantRegion  string
		wantErr     bool
	}{
		{"secretsmanager.us-east-1.amazonaws.com", "secretsmanager", "us-east-1", false},
		{"ssm.eu-west-1.amazonaws.com", "ssm", "eu-west-1", false},
		{"kms.ap-southeast-1.amazonaws.com", "kms", "ap-southeast-1", false},
		{"badformat.amazonaws.com", "", "", true},
	}
	for _, tc := range tests {
		svc, region, err := parseAWSService(tc.fqdn)
		if tc.wantErr {
			if err == nil {
				t.Errorf("expected error for %q, got nil", tc.fqdn)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.fqdn, err)
		}
		if svc != tc.wantService {
			t.Errorf("service: got %q, want %q", svc, tc.wantService)
		}
		if region != tc.wantRegion {
			t.Errorf("region: got %q, want %q", region, tc.wantRegion)
		}
	}
}

func TestParseTencentService(t *testing.T) {
	tests := []struct {
		fqdn    string
		wantSvc string
		wantErr bool
	}{
		{"cvm.tencentcloudapi.com", "cvm", false},
		{"cos.ap-beijing.myqcloud.com", "cos.ap-beijing", false},
		{"example.com", "", true},
		{".tencentcloudapi.com", "", true},
	}
	for _, tc := range tests {
		svc, err := parseTencentService(tc.fqdn)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTencentService(%q): expected error, got nil", tc.fqdn)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseTencentService(%q): unexpected error: %v", tc.fqdn, err)
		}
		if svc != tc.wantSvc {
			t.Errorf("parseTencentService(%q): got %q, want %q", tc.fqdn, svc, tc.wantSvc)
		}
	}
}

func TestSignTencentAt(t *testing.T) {
	t.Setenv("TENCENTCLOUD_SECRET_ID", "test-secret-id")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "test-secret-key")

	req, err := http.NewRequest("POST", "https://cvm.tencentcloudapi.com/", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "cvm.tencentcloudapi.com"
	req.Header.Set("Content-Type", "application/json")

	h := &handler{}
	now := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if err := h.signTencentAt(req, []byte(`{}`), "cvm", now); err != nil {
		t.Fatalf("signTencentAt error: %v", err)
	}

	// Verify X-TC-Timestamp
	wantTS := "1705276800"
	if got := req.Header.Get("X-TC-Timestamp"); got != wantTS {
		t.Errorf("X-TC-Timestamp: got %q, want %q", got, wantTS)
	}

	// Verify Authorization header format
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=test-secret-id/") {
		t.Errorf("Authorization prefix wrong: %q", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host") {
		t.Errorf("Authorization missing SignedHeaders: %q", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Errorf("Authorization missing Signature: %q", auth)
	}

	// Verify exact signature golden value
	wantAuth := "TC3-HMAC-SHA256 Credential=test-secret-id/2024-01-15/cvm/tc3_request, SignedHeaders=content-type;host, Signature=50d89240848344230e0049154b87cb574af5477b758cfe6ab2e45beb16caca85"
	if auth != wantAuth {
		t.Errorf("Authorization: got %q, want %q", auth, wantAuth)
	}
}
