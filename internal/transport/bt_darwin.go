// Copyright (c) 2026 PenEngineering S.R.L
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

// BLE GATT transport for akira-cli on macOS — uses tinygo-org/bluetooth which
// wraps the native CoreBluetooth API.
//
// CoreBluetooth does not expose hardware MAC addresses. The --device flag must
// be either the device's CoreBluetooth peripheral UUID (shown in system
// Bluetooth settings as a UUID like "12345678-1234-...") or the device's
// advertised local name (e.g. "AkiraOS"). The adapter scans until a matching
// device is found or the timeout expires.
//
// Same AkiraOS App Transfer Service protocol as bt_linux.go.
package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

// AkiraOS App Transfer Service GATT UUIDs.
const (
	uuidAppXferSvc = "414b4952-0001-0001-0001-000000000001"
	uuidRXData     = "414b4952-0001-0001-0001-000000000002"
	uuidTXStatus   = "414b4952-0001-0001-0001-000000000003"
	uuidControl    = "414b4952-0001-0001-0001-000000000004"
)

// Control opcodes — must match bt_app_cmd_t in bt_app_transfer.h.
const (
	cmdStart  = byte(0x01)
	cmdAbort  = byte(0x02)
	cmdCommit = byte(0x03)
)

// bt_app_status_t values in notification byte[1].
const (
	appStatusOK          = byte(0x00)
	appStatusCRCFail     = byte(0x02)
	appStatusSizeError   = byte(0x03)
	appStatusInstallFail = byte(0x04)
)

// bleChunkSize is the max write payload per ATT Write Command.
// ATT_MTU=247 (BLE 5 DLE) → 247 - 3 = 244 bytes.
const bleChunkSize = 244

// ─── Status notification ──────────────────────────────────────────────────────

type statusNotif struct{ raw [4]byte }

func parseNotif(b []byte) statusNotif {
	var n statusNotif
	copy(n.raw[:], b)
	return n
}

func (n statusNotif) IsOK() bool     { return n.raw[1] == appStatusOK }
func (n statusNotif) Progress() byte { return n.raw[2] }

func (n statusNotif) Err() error {
	switch n.raw[1] {
	case appStatusOK:
		return nil
	case appStatusCRCFail:
		return fmt.Errorf("device rejected package: CRC mismatch")
	case appStatusSizeError:
		return fmt.Errorf("device rejected package: size mismatch")
	case appStatusInstallFail:
		return fmt.Errorf("device install failed (code 0x%02x)", n.raw[1])
	default:
		return fmt.Errorf("device error 0x%02x", n.raw[1])
	}
}

// ─── Client ──────────────────────────────────────────────────────────────────

// BTClient transfers .akpkg files to an AkiraOS device via BLE GATT.
type BTClient struct {
	address string // CoreBluetooth UUID or advertised local name
	adapter string // unused on macOS — only DefaultAdapter is available
	timeout time.Duration
}

// NewBTClient creates a BTClient. On macOS, address should be the
// CoreBluetooth peripheral UUID or the device's advertised local name.
func NewBTClient(address, adapterID string, timeout int) *BTClient {
	if adapterID == "" {
		adapterID = "hci0"
	}
	return &BTClient{
		address: address,
		adapter: adapterID,
		timeout: time.Duration(timeout) * time.Second,
	}
}

// ─── Install ─────────────────────────────────────────────────────────────────

func (c *BTClient) Install(pkgPath string) (string, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", pkgPath, err)
	}
	appName := appNameFromPath(pkgPath)

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	chars, cleanup, err := c.openChars(ctx)
	if err != nil {
		return "", err
	}
	defer cleanup()

	notifCh, err := subscribeNotify(chars.txStatus)
	if err != nil {
		return "", fmt.Errorf("enable notifications: %w", err)
	}

	// 1. START
	startCmd, err := buildStartCmd(appName, data)
	if err != nil {
		return "", fmt.Errorf("build START: %w", err)
	}
	if _, err := chars.ctrl.Write(startCmd); err != nil {
		return "", fmt.Errorf("control START: %w", err)
	}
	if err := waitForStatus(ctx, notifCh, func(n statusNotif) (bool, error) {
		if !n.IsOK() {
			return false, n.Err()
		}
		return true, nil
	}); err != nil {
		return "", fmt.Errorf("device did not signal READY: %w", err)
	}

	// 2. STREAM chunks (Write Without Response — matches BT_GATT_CHRC_WRITE_WITHOUT_RESP)
	total := len(data)
	for off := 0; off < total; off += bleChunkSize {
		end := off + bleChunkSize
		if end > total {
			end = total
		}
		if _, err := chars.rxData.WriteWithoutResponse(data[off:end]); err != nil {
			_, _ = chars.ctrl.Write([]byte{cmdAbort})
			return "", fmt.Errorf("data write at offset %d: %w", off, err)
		}
		fmt.Printf("\r  uploading ... %3d%%", (end*100)/total)
	}
	fmt.Println()

	// 3. COMMIT
	if _, err := chars.ctrl.Write([]byte{cmdCommit}); err != nil {
		return "", fmt.Errorf("control COMMIT: %w", err)
	}

	return "installed successfully", nil
}

// ─── Connection helpers ───────────────────────────────────────────────────────

type darwinXferChars struct {
	rxData   bluetooth.DeviceCharacteristic
	txStatus bluetooth.DeviceCharacteristic
	ctrl     bluetooth.DeviceCharacteristic
}

func (c *BTClient) openChars(ctx context.Context) (*darwinXferChars, func(), error) {
	a := bluetooth.DefaultAdapter
	if err := a.Enable(); err != nil {
		return nil, nil, fmt.Errorf("enable BLE adapter: %w", err)
	}

	addr, err := c.scanForDevice(ctx, a)
	if err != nil {
		return nil, nil, err
	}

	dev, err := a.Connect(addr, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, nil, fmt.Errorf("connect to device: %w", err)
	}
	cleanup := func() { dev.Disconnect() } //nolint:errcheck

	svcUUID, _ := bluetooth.ParseUUID(uuidAppXferSvc)
	services, err := dev.DiscoverServices([]bluetooth.UUID{svcUUID})
	if err != nil || len(services) == 0 {
		cleanup()
		if err == nil {
			err = fmt.Errorf("service not found — is the device running AkiraOS?")
		}
		return nil, nil, fmt.Errorf("discover app transfer service: %w", err)
	}

	rxUUID, _   := bluetooth.ParseUUID(uuidRXData)
	txUUID, _   := bluetooth.ParseUUID(uuidTXStatus)
	ctrlUUID, _ := bluetooth.ParseUUID(uuidControl)

	rawChars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{rxUUID, txUUID, ctrlUUID})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("discover characteristics: %w", err)
	}

	result := &darwinXferChars{}
	rxFound, txFound, ctrlFound := false, false, false
	for _, ch := range rawChars {
		u := ch.UUID()
		switch {
		case u == rxUUID:
			result.rxData = ch
			rxFound = true
		case u == txUUID:
			result.txStatus = ch
			txFound = true
		case u == ctrlUUID:
			result.ctrl = ch
			ctrlFound = true
		}
	}
	if !rxFound || !txFound || !ctrlFound {
		cleanup()
		return nil, nil, fmt.Errorf("AkiraOS app-transfer characteristics not found on device")
	}

	return result, cleanup, nil
}

// scanForDevice scans until a device matching c.address (by CoreBluetooth UUID
// or advertised local name) is found or ctx expires.
func (c *BTClient) scanForDevice(ctx context.Context, a *bluetooth.Adapter) (bluetooth.Address, error) {
	found := make(chan bluetooth.Address, 1)

	go func() {
		_ = a.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
			addrStr := result.Address.String()
			name := result.LocalName()
			if strings.EqualFold(addrStr, c.address) || strings.EqualFold(name, c.address) {
				select {
				case found <- result.Address:
				default:
				}
				_ = adapter.StopScan()
			}
		})
	}()

	select {
	case <-ctx.Done():
		_ = a.StopScan()
		return bluetooth.Address{}, fmt.Errorf(
			"scan timeout: device %q not found (use CoreBluetooth UUID or advertised device name)",
			c.address,
		)
	case addr := <-found:
		return addr, nil
	}
}

// ─── Notification subscription ────────────────────────────────────────────────

func subscribeNotify(char bluetooth.DeviceCharacteristic) (<-chan []byte, error) {
	ch := make(chan []byte, 256)
	if err := char.EnableNotifications(func(buf []byte) {
		b := make([]byte, len(buf))
		copy(b, buf)
		select {
		case ch <- b:
		default:
		}
	}); err != nil {
		return nil, err
	}
	return ch, nil
}

// ─── Status wait ─────────────────────────────────────────────────────────────

func waitForStatus(ctx context.Context, ch <-chan []byte, pred func(statusNotif) (bool, error)) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("BLE timeout: %w", ctx.Err())
		case b, ok := <-ch:
			if !ok {
				return fmt.Errorf("status channel closed unexpectedly")
			}
			n := parseNotif(b)
			done, err := pred(n)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

func buildStartCmd(appName string, data []byte) ([]byte, error) {
	var hdr struct {
		Name [32]byte
		Size uint32
		CRC  uint32
	}
	copy(hdr.Name[:], appName)
	hdr.Size = uint32(len(data))
	hdr.CRC = crc32.ChecksumIEEE(data)

	var buf bytes.Buffer
	buf.WriteByte(cmdStart)
	if err := binary.Write(&buf, binary.LittleEndian, hdr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func appNameFromPath(pkgPath string) string {
	base := filepath.Base(pkgPath)
	return strings.TrimSuffix(base, ".akpkg")
}
