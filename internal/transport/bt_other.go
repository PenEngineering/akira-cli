// Copyright (c) 2026 PenEngineering S.R.L
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !windows && !darwin

// Stub for platforms without a supported BLE backend.
package transport

import (
	"fmt"
	"time"
)

// BTClient is a no-op stub on platforms without BlueZ.
type BTClient struct {
	address string
	adapter string
	timeout time.Duration
}

// NewBTClient returns a BTClient stub.
func NewBTClient(address, adapterID string, timeout int) *BTClient {
	return &BTClient{
		address: address,
		adapter: adapterID,
		timeout: time.Duration(timeout) * time.Second,
	}
}

// Install always returns an error on non-Linux/Windows platforms.
func (c *BTClient) Install(_ string) (string, error) {
	return "", fmt.Errorf("BLE GATT transport is not supported on this platform")
}
