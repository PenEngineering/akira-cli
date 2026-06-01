/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

// Package transport provides the WiFi HTTP OTA transport for akira-cli install.
package transport

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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

	fi, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", pkgPath, err)
	}

	url := c.baseURL + "/api/apps/install"
	req, err := http.NewRequest(http.MethodPost, url, f)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.ContentLength = fi.Size()
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

// progressReader wraps an io.Reader and calls onProgress with bytes read so far.
type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	onProgress func(read, total int64)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	p.read += int64(n)
	if p.onProgress != nil {
		p.onProgress(p.read, p.total)
	}
	return n, err
}

// OtaUpdate uploads a firmware binary to the device's /upload endpoint.
// firmwarePath should point to zephyr.signed.bin produced by the build.
// onProgress is called periodically with (bytesUploaded, totalBytes); may be nil.
func (c *HTTPClient) OtaUpdate(firmwarePath string, onProgress func(read, total int64)) error {
	f, err := os.Open(firmwarePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", firmwarePath, err)
	}
	defer f.Close()

	// Buffer the multipart body so we can set Content-Length.
	// The Zephyr HTTP server rejects requests with content_length == 0
	// (chunked transfer encoding is not supported). Firmware is ~1 MB — fits in RAM.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filepath.Base(firmwarePath))
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err = io.Copy(part, f); err != nil {
		return fmt.Errorf("buffer firmware: %w", err)
	}
	mw.Close()

	totalSize := int64(buf.Len())
	var reader io.Reader = &buf
	if onProgress != nil {
		reader = &progressReader{r: &buf, total: totalSize, onProgress: onProgress}
	}

	url := c.baseURL + "/upload"

	// Use a longer timeout — firmware uploads can take 60+ seconds.
	uploadTimeout := c.client.Timeout + 120*time.Second
	if uploadTimeout < 180*time.Second {
		uploadTimeout = 180 * time.Second
	}
	uploadClient := &http.Client{Timeout: uploadTimeout}

	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.ContentLength = totalSize
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := uploadClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("device returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if msg := strings.TrimSpace(string(respBody)); msg != "" {
		fmt.Printf("  device: %s\n", msg)
	}
	return nil
}
