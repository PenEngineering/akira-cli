// Copyright (c) 2026 PenEngineering S.R.L
// SPDX-License-Identifier: Apache-2.0

//go:build !cgo && !windows

// Stub for builds without CGo (e.g. cross-compiled Windows binaries from Linux).
// USB HID requires CGo; build natively with CGO_ENABLED=1 for USB support.
package transport

import (
	"fmt"
	"time"
)

// USBClient is a no-op stub when CGo is unavailable.
type USBClient struct {
	timeout time.Duration
}

// NewUSBClient returns a USBClient stub.
func NewUSBClient(timeout int) *USBClient {
	return &USBClient{timeout: time.Duration(timeout) * time.Second}
}

// Install always returns an error when CGo is not enabled.
func (c *USBClient) Install(_ string) (string, error) {
	return "", fmt.Errorf("USB transport requires CGo — build natively with CGO_ENABLED=1 and libusb/hidapi installed")
}
