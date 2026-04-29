/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

// Package transport provides the WiFi HTTP OTA transport for akira-cli install.
package transport

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// HTTPClient sends .akpkg files to an AkiraOS device's OTA endpoint.
type HTTPClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPClient creates an HTTPClient targeting device (IP or hostname).
// timeout is the per-request HTTP timeout in seconds.
func NewHTTPClient(device, token string, timeout int) *HTTPClient {
	// Normalise: strip trailing slash, prepend scheme if missing.
	addr := strings.TrimRight(device, "/")
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return &HTTPClient{
		baseURL: addr,
		token:   token,
		client:  &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
}

// Install uploads pkgPath to the device's /api/apps/install endpoint.
// Returns the trimmed response body on success.
func (c *HTTPClient) Install(pkgPath string) (string, error) {
	f, err := os.Open(pkgPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", pkgPath, err)
	}
	defer f.Close()

	url := c.baseURL + "/api/apps/install"
	req, err := http.NewRequest(http.MethodPost, url, f)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("device returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return strings.TrimSpace(string(body)), nil
}
