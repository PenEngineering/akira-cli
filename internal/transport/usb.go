// Copyright (c) 2026 PenEngineering S.R.L
// SPDX-License-Identifier: Apache-2.0

//go:build cgo && !windows

// USB HID transport for akira-cli — targets the AkiraOS WebHID protocol.
//
// AkiraOS exposes a raw HID interface (Report ID 3, Usage Page 0xFF60 / Usage 0x61)
// on VID 0x2fe3 / PID 0x0001.  Each report is 64 bytes: 1 report-ID byte + 63
// payload bytes.
//
// Packet layout (63 payload bytes, from hid_app_handler.h):
//
//	Byte 0   CMD    — command / response code
//	Byte 1   SEQ    — sequence number (echoed in response)
//	Byte 2   FLAGS  — upper nibble = status (0=OK); bit 0 = more-data
//	Byte 3   LEN_LO — low byte of data length
//	Byte 4   LEN_HI — high byte of data length
//	Bytes 5..62 — data payload (max 58 bytes per packet)
//
// Install protocol:
//  1. INSTALL_BEGIN (0x40): payload = [total_size: u32 LE][name: null-terminated]
//  2. INSTALL_CHUNK (0x41): payload = raw chunk (up to 58 bytes per packet)
//  3. INSTALL_END   (0x42): no payload
//
// Each command receives a response with CMD|0x80 echoed back; status in
// upper nibble of FLAGS byte (0 = OK).
package transport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	hid "github.com/sstallion/go-hid"
)

// AkiraOS USB identifiers.
const (
	usbVID = uint16(0x2FE3)
	usbPID = uint16(0x0001)

	// Raw HID interface — QMK RAW HID / WebHID convention.
	usbHIDUsagePage = uint16(0xFF60)
	usbHIDUsage     = uint16(0x61)

	usbHIDReportID   = byte(3)
	usbHIDReportSize = 64 // 1 (report ID) + 63 (payload)
	usbHIDDataSize   = 58 // HID_PKT_PAYLOAD_SIZE
)

// Command bytes (host → device).
const (
	usbCmdInstallBegin = byte(0x40)
	usbCmdInstallChunk = byte(0x41)
	usbCmdInstallEnd   = byte(0x42)
	usbCmdInstallAbort = byte(0x43)
)

// Packet field offsets within the 63-byte HID payload.
const (
	hidPktCmd     = 0
	hidPktSeq     = 1
	hidPktFlags   = 2
	hidPktLenLo   = 3
	hidPktLenHi   = 4
	hidPktPayload = 5
)

// USBClient installs .akpkg files on an AkiraOS device over USB HID.
type USBClient struct {
	timeout time.Duration
	seq     uint8
}

// NewUSBClient creates a USBClient with the given timeout in seconds.
func NewUSBClient(timeout int) *USBClient {
	return &USBClient{timeout: time.Duration(timeout) * time.Second}
}

// Install streams pkgPath to the device over USB HID and returns a confirmation string.
func (c *USBClient) Install(pkgPath string) (string, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", pkgPath, err)
	}
	appName := appNameFromPath(pkgPath)

	if err := hid.Init(); err != nil {
		return "", fmt.Errorf("hidapi init: %w", err)
	}
	defer hid.Exit() //nolint:errcheck

	dev, err := openUSBDevice()
	if err != nil {
		return "", err
	}
	defer dev.Close() //nolint:errcheck

	// INSTALL_BEGIN: [total_size: u32 LE][name: null-terminated]
	beginPayload := make([]byte, 4+len(appName)+1)
	binary.LittleEndian.PutUint32(beginPayload, uint32(len(data)))
	copy(beginPayload[4:], appName)
	if err := c.command(dev, usbCmdInstallBegin, beginPayload); err != nil {
		return "", fmt.Errorf("INSTALL_BEGIN: %w", err)
	}

	// INSTALL_CHUNK — up to 58 bytes of app data per HID packet.
	total := len(data)
	for off := 0; off < total; off += usbHIDDataSize {
		end := off + usbHIDDataSize
		if end > total {
			end = total
		}
		if err := c.command(dev, usbCmdInstallChunk, data[off:end]); err != nil {
			_ = c.write(dev, usbCmdInstallAbort, c.nextSeq(), nil)
			return "", fmt.Errorf("INSTALL_CHUNK at offset %d: %w", off, err)
		}
		fmt.Printf("\r  uploading ... %3d%%", (end*100)/total)
	}
	fmt.Println()

	// INSTALL_END
	if err := c.command(dev, usbCmdInstallEnd, nil); err != nil {
		return "", fmt.Errorf("INSTALL_END: %w", err)
	}

	return "installed successfully", nil
}

// openUSBDevice finds the AkiraOS raw HID interface.
// It prefers the interface matching Usage Page 0xFF60 / Usage 0x61; on Linux with
// the hidraw backend the usage page may not be populated, so it falls back to the
// first VID/PID match.
func openUSBDevice() (*hid.Device, error) {
	var exactPath, fallbackPath string
	if err := hid.Enumerate(usbVID, usbPID, func(info *hid.DeviceInfo) error {
		if info.UsagePage == usbHIDUsagePage && info.Usage == usbHIDUsage {
			exactPath = info.Path
		} else if fallbackPath == "" {
			fallbackPath = info.Path
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("USB enumerate: %w", err)
	}

	path := exactPath
	if path == "" {
		path = fallbackPath
	}
	if path == "" {
		return nil, fmt.Errorf(
			"AkiraOS device not found (VID=0x%04x PID=0x%04x) — is it connected and powered on?",
			usbVID, usbPID,
		)
	}
	dev, err := hid.OpenPath(path)
	if err != nil {
		if isPermissionErr(err) {
			return nil, fmt.Errorf(
				"permission denied opening %s\n\n"+
					"On Linux, hidraw devices require a udev rule for non-root access.\n"+
					"Run once (from the akira-cli source directory):\n\n"+
					"  make udev-install\n\n"+
					"Or manually:\n"+
					"  sudo cp scripts/99-akiraos.rules /etc/udev/rules.d/\n"+
					"  sudo udevadm control --reload-rules && sudo udevadm trigger\n"+
					"  sudo usermod -aG plugdev $USER   # then log out and back in",
				path,
			)
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return dev, nil
}

// isPermissionErr returns true when err indicates an EACCES / permission denied
// condition.  hidapi surfaces OS errors as plain strings, so we check both the
// typed os.ErrPermission sentinel and the error message text.
func isPermissionErr(err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Permission denied") || strings.Contains(msg, "EACCES")
}

// command sends a HID OUT report and waits for the matching IN response.
func (c *USBClient) command(dev *hid.Device, cmd byte, payload []byte) error {
	seq := c.nextSeq()
	if err := c.write(dev, cmd, seq, payload); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return c.readResponse(dev, cmd, seq)
}

// write sends a single 64-byte HID OUT report.
// buf: [reportID | CMD | SEQ | FLAGS | LEN_LO | LEN_HI | payload...]
func (c *USBClient) write(dev *hid.Device, cmd, seq byte, payload []byte) error {
	buf := make([]byte, usbHIDReportSize)
	buf[0] = usbHIDReportID
	buf[1+hidPktCmd] = cmd
	buf[1+hidPktSeq] = seq
	buf[1+hidPktFlags] = 0
	n := len(payload)
	if n > usbHIDDataSize {
		n = usbHIDDataSize
	}
	buf[1+hidPktLenLo] = byte(n)
	buf[1+hidPktLenHi] = byte(n >> 8)
	copy(buf[1+hidPktPayload:], payload[:n])
	_, err := dev.Write(buf)
	return err
}

// readResponse reads IN reports until it finds one matching cmd and seq.
// Status is in the upper nibble of the FLAGS byte: (flags >> 4) != 0 means error.
func (c *USBClient) readResponse(dev *hid.Device, cmd, seq byte) error {
	buf := make([]byte, usbHIDReportSize)

	for {
		n, err := dev.ReadWithTimeout(buf, c.timeout)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		if n == 0 {
			continue // no data yet, keep waiting
		}

		// hidapi may or may not include the report ID as the first byte;
		// strip it if present so data[0] == HID_PKT_CMD.
		data := buf[:n]
		if len(data) > 0 && data[0] == usbHIDReportID {
			data = data[1:]
		}
		if len(data) < 3 {
			continue // too short to be a valid response
		}

		if data[hidPktCmd] != (cmd|0x80) || data[hidPktSeq] != seq {
			continue // not our response
		}
		if status := data[hidPktFlags] >> 4; status != 0 {
			return fmt.Errorf("device error 0x%02x for command 0x%02x", status, cmd)
		}
		return nil
	}
}

func (c *USBClient) nextSeq() byte {
	s := c.seq
	c.seq++
	return s
}
