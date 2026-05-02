package main

import (
	"testing"
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
