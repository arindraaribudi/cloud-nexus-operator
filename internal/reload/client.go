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

// Package reload provides an HTTP client for the frpc admin API hot-reload endpoint.
package reload

import (
	"context"
	"fmt"
	"net/http"
)

// Client calls the frpc admin API to trigger a hot reload.
type Client struct {
	httpClient *http.Client
}

// New returns a Client using the provided http.Client. Pass nil to use the default.
func New(hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{httpClient: hc}
}

// Reload calls POST http://{host}:{port}/api/reload with HTTP Basic Auth (frpc v0.61+).
// token is used as the password with an empty username.
// Returns nil on HTTP 200, an error otherwise.
func (c *Client) Reload(ctx context.Context, host string, port int32, token string) error {
	url := fmt.Sprintf("http://%s:%d/api/reload", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build reload request: %w", err)
	}
	if token != "" {
		req.SetBasicAuth("", token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reload request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reload returned HTTP %d", resp.StatusCode)
	}
	return nil
}
