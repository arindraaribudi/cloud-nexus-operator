package main

import (
	"strings"
	"testing"
)

func TestParseUpstream_Valid(t *testing.T) {
	tests := []struct {
		path     string
		wantHost string
		wantRest string
		wantOK   bool
	}{
		{
			path:     "/v2/asia-southeast3-docker.pkg.dev/my-project/my-app/manifests/latest",
			wantHost: "asia-southeast3-docker.pkg.dev",
			wantRest: "my-project/my-app/manifests/latest",
			wantOK:   true,
		},
		{
			path:     "/v2/123456789.dkr.ecr.us-east-1.amazonaws.com/my-image/blobs/sha256:abc",
			wantHost: "123456789.dkr.ecr.us-east-1.amazonaws.com",
			wantRest: "my-image/blobs/sha256:abc",
			wantOK:   true,
		},
		{
			path:     "/v2/",
			wantHost: "",
			wantRest: "",
			wantOK:   false,
		},
		{
			path:     "/v2/nohost/manifests/tag",
			wantHost: "",
			wantRest: "",
			wantOK:   false,
		},
	}
	for _, tc := range tests {
		host, rest, ok := parseUpstream(tc.path)
		if ok != tc.wantOK {
			t.Errorf("path=%q: got ok=%v, want %v", tc.path, ok, tc.wantOK)
		}
		if host != tc.wantHost {
			t.Errorf("path=%q: got host=%q, want %q", tc.path, host, tc.wantHost)
		}
		if rest != tc.wantRest {
			t.Errorf("path=%q: got rest=%q, want %q", tc.path, rest, tc.wantRest)
		}
	}
}

func TestAuthScheme(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"asia-southeast3-docker.pkg.dev", "gcp"},
		{"gcr.io", "gcp"},
		{"us.gcr.io", "gcp"},
		{"123456.dkr.ecr.us-east-1.amazonaws.com", "ecr"},
		{"ghcr.io", ""},
		{"registry-1.docker.io", ""},
		{"example.com", ""},
	}
	for _, tc := range tests {
		got := authScheme(tc.host)
		if got != tc.want {
			t.Errorf("host=%q: got %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestStripPrefix_Applied(t *testing.T) {
	// With STRIP_PREFIX=/spoke1/registry, path /spoke1/registry/v2/host.io/img/manifests/tag
	// should be routed as if path was /v2/host.io/img/manifests/tag.
	// We test parseUpstream on the stripped path.
	raw := "/spoke1/registry/v2/asia-southeast3-docker.pkg.dev/proj/img/manifests/latest"
	prefix := "/spoke1/registry"
	stripped := strings.TrimPrefix(raw, prefix)
	// stripped = /v2/asia-southeast3-docker.pkg.dev/proj/img/manifests/latest
	host, rest, ok := parseUpstream(stripped)
	if !ok {
		t.Fatal("expected ok=true after stripping prefix")
	}
	if host != "asia-southeast3-docker.pkg.dev" {
		t.Errorf("host: got %q, want %q", host, "asia-southeast3-docker.pkg.dev")
	}
	if rest != "proj/img/manifests/latest" {
		t.Errorf("rest: got %q, want %q", rest, "proj/img/manifests/latest")
	}
}

func TestStripPrefix_EmptyPrefix(t *testing.T) {
	// When STRIP_PREFIX is empty, /v2/host.io/img/... works unchanged.
	raw := "/v2/asia-southeast3-docker.pkg.dev/proj/img/manifests/latest"
	host, _, ok := parseUpstream(raw)
	if !ok {
		t.Fatal("expected ok=true with no prefix")
	}
	if host != "asia-southeast3-docker.pkg.dev" {
		t.Errorf("host: got %q", host)
	}
}
