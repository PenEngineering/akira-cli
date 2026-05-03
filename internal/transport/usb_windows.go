// Copyright (c) 2026 PenEngineering S.R.L
// SPDX-License-Identifier: Apache-2.0

//go:build windows

// USB HID transport for Windows — pure Go, no CGo required.
//
// Uses Windows SetupAPI (setupapi.dll) to enumerate HID devices and hid.dll
// for attribute queries.  Reports are sent/received via kernel32 WriteFile /
// ReadFile with overlapped I/O so a timeout can be enforced.
//
// Protocol constants are identical to the CGo implementation (usb.go).
package transport

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// AkiraOS USB identifiers (same as usb.go)
// ---------------------------------------------------------------------------

const (
	usbVID = uint16(0x2FE3)
	usbPID = uint16(0x0001)

	usbHIDUsagePage = uint16(0xFF60)
	usbHIDUsage     = uint16(0x61)

	usbHIDReportID   = byte(3)
	usbHIDReportSize = 64
	usbHIDDataSize   = 58
)

const (
	usbCmdInstallBegin = byte(0x40)
	usbCmdInstallChunk = byte(0x41)
	usbCmdInstallEnd   = byte(0x42)
	usbCmdInstallAbort = byte(0x43)
)

const (
	hidPktCmd     = 0
	hidPktSeq     = 1
	hidPktFlags   = 2
	hidPktLenLo   = 3
	hidPktLenHi   = 4
	hidPktPayload = 5
)

// ---------------------------------------------------------------------------
// Windows API declarations
// ---------------------------------------------------------------------------

var (
	setupapiDLL = windows.NewLazySystemDLL("setupapi.dll")
	hidDLL      = windows.NewLazySystemDLL("hid.dll")

	procSetupDiGetClassDevsW             = setupapiDLL.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces      = setupapiDLL.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = setupapiDLL.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList     = setupapiDLL.NewProc("SetupDiDestroyDeviceInfoList")

	procHidD_GetAttributes     = hidDLL.NewProc("HidD_GetAttributes")
	procHidD_GetPreparsedData  = hidDLL.NewProc("HidD_GetPreparsedData")
	procHidD_FreePreparsedData = hidDLL.NewProc("HidD_FreePreparsedData")
	procHidP_GetCaps           = hidDLL.NewProc("HidP_GetCaps")
	procHidD_SetOutputReport   = hidDLL.NewProc("HidD_SetOutputReport")
)

// Windows constants
const (
	digcfPresent         = 0x00000002
	digcfDeviceInterface = 0x00000010
	invalidHandle        = ^uintptr(0)
)

// HID interface class GUID: {4d1e55b2-f16f-11cf-88cb-001111000030}
var hidGUID = windows.GUID{
	Data1: 0x4d1e55b2,
	Data2: 0xf16f,
	Data3: 0x11cf,
	Data4: [8]byte{0x88, 0xcb, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30},
}

type spDeviceInterfaceData struct {
	Size               uint32
	InterfaceClassGUID windows.GUID
	Flags              uint32
	Reserved           uintptr
}

type hidAttributes struct {
	Size      uint32
	VendorID  uint16
	ProductID uint16
	Version   uint16
}

// hidpCaps mirrors the Windows HIDP_CAPS structure.
type hidpCaps struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

// ---------------------------------------------------------------------------
// USBClient
// ---------------------------------------------------------------------------

// USBClient installs .akpkg apps on an AkiraOS device over USB HID.
type USBClient struct {
	timeout time.Duration
	seq     uint8
}

// NewUSBClient creates a USBClient with the given timeout in seconds.
func NewUSBClient(timeout int) *USBClient {
	return &USBClient{timeout: time.Duration(timeout) * time.Second}
}

// Install streams pkgPath to the device over USB HID.
func (c *USBClient) Install(pkgPath string) (string, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", pkgPath, err)
	}
	appName := appNameFromPath(pkgPath)

	dev, err := openWinHIDDevice()
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(dev) //nolint:errcheck

	// INSTALL_BEGIN: [total_size: u32 LE][name: null-terminated]
	beginPayload := make([]byte, 4+len(appName)+1)
	binary.LittleEndian.PutUint32(beginPayload, uint32(len(data)))
	copy(beginPayload[4:], appName)
	if err := c.command(dev, usbCmdInstallBegin, beginPayload); err != nil {
		return "", fmt.Errorf("INSTALL_BEGIN: %w", err)
	}

	// INSTALL_CHUNK
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

// ---------------------------------------------------------------------------
// Device enumeration
// ---------------------------------------------------------------------------

func openWinHIDDevice() (windows.Handle, error) {
	devInfo, _, err := procSetupDiGetClassDevsW.Call(
		uintptr(unsafe.Pointer(&hidGUID)),
		0, 0,
		digcfPresent|digcfDeviceInterface,
	)
	if devInfo == invalidHandle {
		return windows.InvalidHandle, fmt.Errorf("SetupDiGetClassDevsW: %w", err)
	}
	defer procSetupDiDestroyDeviceInfoList.Call(devInfo) //nolint:errcheck

	var ifaceData spDeviceInterfaceData
	ifaceData.Size = uint32(unsafe.Sizeof(ifaceData))

	for idx := uint32(0); ; idx++ {
		ret, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			devInfo, 0,
			uintptr(unsafe.Pointer(&hidGUID)),
			uintptr(idx),
			uintptr(unsafe.Pointer(&ifaceData)),
		)
		if ret == 0 {
			break // no more interfaces
		}

		path, err := getDevicePath(devInfo, &ifaceData)
		if err != nil {
			continue
		}

		// Open with zero desired-access for attribute querying — works even for
		// interfaces that Windows has locked (keyboard, mouse).
		hq, err := openQueryHandle(path)
		if err != nil {
			continue
		}
		matches := matchesVIDPID(hq)
		windows.CloseHandle(hq) //nolint:errcheck

		if !matches {
			continue
		}

		// Re-open with full access for actual I/O.
		h, err := openRawHandle(path)
		if err != nil {
			return windows.InvalidHandle, fmt.Errorf("open raw HID interface: %w", err)
		}
		return h, nil
	}

	return windows.InvalidHandle, fmt.Errorf(
		"AkiraOS device not found (VID=0x%04x PID=0x%04x) — is it connected?",
		usbVID, usbPID,
	)
}

func getDevicePath(devInfo uintptr, ifaceData *spDeviceInterfaceData) (string, error) {
	// First call: get required size
	var requiredSize uint32
	procSetupDiGetDeviceInterfaceDetailW.Call( //nolint:errcheck
		devInfo,
		uintptr(unsafe.Pointer(ifaceData)),
		0, 0,
		uintptr(unsafe.Pointer(&requiredSize)),
		0,
	)
	if requiredSize == 0 {
		return "", fmt.Errorf("no detail size")
	}

	// Detail struct: cbSize (DWORD=4) + DevicePath (WCHAR[]).
	// On 64-bit Windows the struct sizeof is 8 (4+2+2 alignment padding).
	// cbSize must reflect that — hardcode 8 for amd64.
	buf := make([]byte, requiredSize)
	const cbSize = uint32(8) // always amd64
	*(*uint32)(unsafe.Pointer(&buf[0])) = cbSize

	ret, _, err := procSetupDiGetDeviceInterfaceDetailW.Call(
		devInfo,
		uintptr(unsafe.Pointer(ifaceData)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(requiredSize),
		0, 0,
	)
	if ret == 0 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetailW: %w", err)
	}

	// DevicePath starts at buf[4] as a null-terminated UTF-16 string
	pathUTF16 := (*[32768]uint16)(unsafe.Pointer(&buf[4]))[: (requiredSize-4)/2 : (requiredSize-4)/2]
	return windows.UTF16ToString(pathUTF16), nil
}

// openQueryHandle opens a device with zero desired-access so that attribute
// queries (HidD_GetAttributes, HidP_GetCaps) work even on interfaces that
// Windows has exclusively locked (keyboard, mouse).
func openQueryHandle(path string) (windows.Handle, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(
		pathPtr,
		0, // no access — query only
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
}

func openRawHandle(path string) (windows.Handle, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
}

func matchesVIDPID(h windows.Handle) bool {
	var attrs hidAttributes
	attrs.Size = uint32(unsafe.Sizeof(attrs))
	ret, _, _ := procHidD_GetAttributes.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&attrs)),
	)
	if ret == 0 || attrs.VendorID != usbVID || attrs.ProductID != usbPID {
		return false
	}
	// Also verify usage page so we select the raw HID interface (0xFF60)
	// rather than the keyboard/mouse interface that shares the same VID/PID.
	var preparsed uintptr
	ret, _, _ = procHidD_GetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&preparsed)))
	if ret == 0 {
		return false
	}
	defer procHidD_FreePreparsedData.Call(preparsed) //nolint:errcheck

	var caps hidpCaps
	// HidP_GetCaps returns NTSTATUS; HIDP_STATUS_SUCCESS = 0x00110000
	const hidpStatusSuccess = uintptr(0x00110000)
	ret, _, _ = procHidP_GetCaps.Call(preparsed, uintptr(unsafe.Pointer(&caps)))
	return ret == hidpStatusSuccess && caps.UsagePage == usbHIDUsagePage
}

// ---------------------------------------------------------------------------
// HID report read / write with overlapped I/O
// ---------------------------------------------------------------------------

func (c *USBClient) command(dev windows.Handle, cmd byte, payload []byte) error {
	seq := c.nextSeq()
	if err := c.write(dev, cmd, seq, payload); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return c.readResponse(dev, cmd, seq)
}

func (c *USBClient) write(dev windows.Handle, cmd, seq byte, payload []byte) error {
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

	// AkiraOS uses Zephyr's new USBD HID API which receives OUT reports via the
	// set_report callback (i.e. SET_REPORT control transfer), not via an interrupt
	// OUT endpoint.  HidD_SetOutputReport issues a SET_REPORT control request and
	// is synchronous — no overlapped I/O needed.
	ret, _, err := procHidD_SetOutputReport.Call(
		uintptr(dev),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if ret == 0 {
		return fmt.Errorf("HidD_SetOutputReport: %w", err)
	}
	return nil
}

func (c *USBClient) readResponse(dev windows.Handle, cmd, seq byte) error {
	deadline := time.Now().Add(c.timeout)
	buf := make([]byte, usbHIDReportSize)

	for time.Now().Before(deadline) {
		var ov windows.Overlapped
		ev, err := windows.CreateEvent(nil, 1, 0, nil)
		if err != nil {
			return fmt.Errorf("CreateEvent: %w", err)
		}
		ov.HEvent = ev

		var nRead uint32
		err = windows.ReadFile(dev, buf, &nRead, &ov)
		if err == windows.ERROR_IO_PENDING {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				windows.CancelIoEx(dev, &ov) //nolint:errcheck
				windows.CloseHandle(ev)      //nolint:errcheck
				return fmt.Errorf("timeout waiting for response to command 0x%02x", cmd)
			}
			waitMs := uint32(remaining.Milliseconds())
			wret, werr := windows.WaitForSingleObject(ev, waitMs)
			if wret == uint32(windows.WAIT_TIMEOUT) {
				windows.CancelIoEx(dev, &ov) //nolint:errcheck
				windows.CloseHandle(ev)      //nolint:errcheck
				return fmt.Errorf("timeout waiting for response to command 0x%02x", cmd)
			}
			if werr != nil {
				windows.CloseHandle(ev) //nolint:errcheck
				return fmt.Errorf("wait: %w", werr)
			}
			err = windows.GetOverlappedResult(dev, &ov, &nRead, false)
		}
		windows.CloseHandle(ev) //nolint:errcheck
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		data := buf[:nRead]
		// Windows HID driver always prepends the report ID
		if len(data) > 0 && data[0] == usbHIDReportID {
			data = data[1:]
		}
		if len(data) < 3 {
			continue
		}
		if data[hidPktCmd] != (cmd|0x80) || data[hidPktSeq] != seq {
			continue
		}
		if status := data[hidPktFlags] >> 4; status != 0 {
			return fmt.Errorf("device error 0x%02x for command 0x%02x", status, cmd)
		}
		return nil
	}
	return fmt.Errorf("timeout waiting for response to command 0x%02x", cmd)
}

func (c *USBClient) nextSeq() byte {
	s := c.seq
	c.seq++
	return s
}
