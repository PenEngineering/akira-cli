// Copyright (c) 2026 PenEngineering S.R.L
// SPDX-License-Identifier: Apache-2.0

//go:build linux

// BLE GATT transport for akira-cli — targets the AkiraOS App Transfer Service.
//
// Service definition (from bt_app_transfer.h / bt_app_transfer.c):
//
//Service  414b4952-0001-0001-0001-000000000001
//RX Data  414b4952-0001-0001-0001-000000000002  Write Without Response <- app chunks
//TX Status 414b4952-0001-0001-0001-000000000003  Notify                <- 4-byte status
//Control  414b4952-0001-0001-0001-000000000004  Write                 <- opcodes
//
// TX Status notification layout (4 bytes, from send_status() in firmware):
//
//[0] bt_app_xfer_state_t  -- current transfer state
//[1] bt_app_status_t      -- 0x00 = OK; non-zero = error code
//[2] progress %           -- 0-100
//[3] reserved
//
// Control command protocol:
//  1. Write cmdStart + bt_app_xfer_header{name[32], total_size LE, crc32 LE}
//  2. Wait for notification: status == 0x00 (device transitions to RECEIVING)
//  3. Write .akpkg chunks to RX Data (Write Without Response)
//  4. Write cmdCommit
//  5. Wait for notification: status == 0x00 && progress == 100
//
// NOTE: opcode values must match bt_app_cmd_t / bt_app_status_t in bt_app_transfer.h.
package transport

import (
"bytes"
"context"
"encoding/binary"
"fmt"
"hash/crc32"
"os"
"strings"
"time"

"github.com/godbus/dbus/v5"
"github.com/muka/go-bluetooth/api"
"github.com/muka/go-bluetooth/bluez/profile/adapter"
"github.com/muka/go-bluetooth/bluez/profile/device"
"github.com/muka/go-bluetooth/bluez/profile/gatt"
)

// AkiraOS App Transfer Service GATT UUIDs.
const (
uuidRXData   = "414b4952-0001-0001-0001-000000000002"
uuidTXStatus = "414b4952-0001-0001-0001-000000000003"
uuidControl  = "414b4952-0001-0001-0001-000000000004"
)

// Control opcodes -- must match bt_app_cmd_t in bt_app_transfer.h.
const (
cmdStart  = byte(0x01) // [0x01 | bt_app_xfer_header{name[32], size[4], crc[4]}]
cmdAbort  = byte(0x02) // [0x02]
cmdCommit = byte(0x03) // [0x03]
)

// bt_app_status_t values carried in notification byte[1].
const (
appStatusOK          = byte(0x00)
appStatusCRCFail     = byte(0x02)
appStatusSizeError   = byte(0x03)
appStatusInstallFail = byte(0x04)
)

// bleChunkSize is the max Write Without Response payload.
// ATT_MTU=247 (BLE 5 DLE) -> 247 - 3 (ATT header) = 244 bytes.
const bleChunkSize = 244

// ---- Status notification ----------------------------------------------------

// statusNotif is the decoded 4-byte TX Status notification payload.
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

// ---- Client -----------------------------------------------------------------

// BTClient transfers .akpkg files and manages apps on an AkiraOS device via BLE GATT.
type BTClient struct {
address string
adapter string
timeout time.Duration
}

// NewBTClient creates a BTClient targeting address (Bluetooth MAC, e.g. "AA:BB:CC:DD:EE:FF").
// adapterID defaults to "hci0" when empty.
func NewBTClient(address, adapterID string, timeout int) *BTClient {
if adapterID == "" {
adapterID = "hci0"
}
return &BTClient{
address: strings.ToUpper(address),
adapter: adapterID,
timeout: time.Duration(timeout) * time.Second,
}
}

// Install streams pkgPath to the device over BLE GATT and returns a confirmation string.
func (c *BTClient) Install(pkgPath string) (string, error) {
data, err := os.ReadFile(pkgPath)
if err != nil {
return "", fmt.Errorf("open %s: %w", pkgPath, err)
}
appName := appNameFromPath(pkgPath)

ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
defer cancel()

chars, conn, cleanup, err := c.openChars(ctx)
if err != nil {
return "", err
}
defer cleanup()

statusCh, cancelWatch, err := watchNotify(conn, chars.txStatusPath)
if err != nil {
return "", fmt.Errorf("notify subscribe: %w", err)
}
defer cancelWatch()
if err := chars.txStatus.StartNotify(); err != nil {
return "", fmt.Errorf("StartNotify: %w", err)
}
defer chars.txStatus.StopNotify() //nolint:errcheck

// 1. START
startCmd, err := buildStartCmd(appName, data)
if err != nil {
return "", fmt.Errorf("build START: %w", err)
}
if err := chars.ctrl.WriteValue(startCmd, nil); err != nil {
return "", fmt.Errorf("control START: %w", err)
}
// Wait for READY: device sends send_status(BT_APP_STATUS_OK, 0) after
// allocating the heap buffer and entering RECEIVING state.
if err := waitForStatus(ctx, statusCh, func(n statusNotif) (bool, error) {
if !n.IsOK() {
return false, n.Err()
}
return true, nil
}); err != nil {
return "", fmt.Errorf("device did not signal READY: %w", err)
}

// 2. STREAM chunks
total := len(data)
// "command" type -> Write Without Response; matches BT_GATT_CHRC_WRITE_WITHOUT_RESP.
writeOpts := map[string]interface{}{"type": "command"}
for off := 0; off < total; off += bleChunkSize {
end := off + bleChunkSize
if end > total {
end = total
}
if err := chars.rxData.WriteValue(data[off:end], writeOpts); err != nil {
_ = chars.ctrl.WriteValue([]byte{cmdAbort}, nil)
return "", fmt.Errorf("data write at offset %d: %w", off, err)
}
fmt.Printf("\r  uploading ... %3d%%", (end*100)/total)
}
fmt.Println()

// 3. COMMIT
if err := chars.ctrl.WriteValue([]byte{cmdCommit}, nil); err != nil {
return "", fmt.Errorf("control COMMIT: %w", err)
}

return "installed successfully", nil
}

// ---- START command builder --------------------------------------------------

// buildStartCmd serialises [cmdStart | bt_app_xfer_header].
//
// Matches the firmware struct:
//
//struct bt_app_xfer_header {
//    char     name[32];        // app name hint (manifest overrides on device)
//    uint32_t total_size;
//    uint32_t expected_crc;    // CRC32-IEEE of the whole package
//};
//
// crc32.ChecksumIEEE matches Zephyr crc32_ieee_update(0, data, len):
// both use poly 0xEDB88320, init 0xFFFFFFFF, output XOR 0xFFFFFFFF.
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

// ---- Connection & GATT helpers ----------------------------------------------

type xferChars struct {
rxData       *gatt.GattCharacteristic1
txStatus     *gatt.GattCharacteristic1
txStatusPath dbus.ObjectPath
ctrl         *gatt.GattCharacteristic1
}

func (c *BTClient) openChars(ctx context.Context) (*xferChars, *dbus.Conn, func(), error) {
dev, err := c.connectDevice(ctx)
if err != nil {
return nil, nil, nil, err
}
conn, err := dbus.SystemBus()
if err != nil {
dev.Disconnect() //nolint:errcheck
return nil, nil, nil, fmt.Errorf("dbus: %w", err)
}
chars, err := discoverChars(conn, dev.Path())
if err != nil {
dev.Disconnect() //nolint:errcheck
return nil, nil, nil, err
}
return chars, conn, func() { dev.Disconnect() }, nil //nolint:errcheck
}

func (c *BTClient) connectDevice(ctx context.Context) (*device.Device1, error) {
a, err := api.GetAdapter(c.adapter)
if err != nil {
return nil, fmt.Errorf("get adapter %s: %w", c.adapter, err)
}
dev, err := a.GetDeviceByAddress(c.address)
if err != nil {
dev, err = c.scanForDevice(ctx, a)
if err != nil {
return nil, err
}
}
if err := dev.Connect(); err != nil {
return nil, fmt.Errorf("connect %s: %w", c.address, err)
}
return dev, awaitServicesResolved(ctx, dev)
}

func (c *BTClient) scanForDevice(ctx context.Context, a *adapter.Adapter1) (*device.Device1, error) {
if err := a.StartDiscovery(); err != nil {
return nil, fmt.Errorf("start discovery: %w", err)
}
defer a.StopDiscovery() //nolint:errcheck

tick := time.NewTicker(500 * time.Millisecond)
defer tick.Stop()
for {
select {
case <-ctx.Done():
return nil, fmt.Errorf("scan timeout: device %s not found", c.address)
case <-tick.C:
if dev, err := a.GetDeviceByAddress(c.address); err == nil {
return dev, nil
}
}
}
}

func awaitServicesResolved(ctx context.Context, dev *device.Device1) error {
tick := time.NewTicker(250 * time.Millisecond)
defer tick.Stop()
for {
select {
case <-ctx.Done():
return fmt.Errorf("timeout: GATT services did not resolve")
case <-tick.C:
props, err := dev.GetProperties()
if err == nil && props.ServicesResolved {
return nil
}
}
}
}

func discoverChars(conn *dbus.Conn, devPath dbus.ObjectPath) (*xferChars, error) {
obj := conn.Object("org.bluez", "/")
var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
if err := obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects); err != nil {
return nil, fmt.Errorf("GetManagedObjects: %w", err)
}
prefix := string(devPath)
result := &xferChars{}
for path, ifaces := range objects {
if !strings.HasPrefix(string(path), prefix) {
continue
}
ci, ok := ifaces["org.bluez.GattCharacteristic1"]
if !ok {
continue
}
uuid, _ := ci["UUID"].Value().(string)
switch strings.ToLower(uuid) {
case uuidRXData:
c, err := gatt.NewGattCharacteristic1(path)
if err != nil {
return nil, err
}
result.rxData = c
case uuidTXStatus:
c, err := gatt.NewGattCharacteristic1(path)
if err != nil {
return nil, err
}
result.txStatus = c
result.txStatusPath = path
case uuidControl:
c, err := gatt.NewGattCharacteristic1(path)
if err != nil {
return nil, err
}
result.ctrl = c
}
}
if result.rxData == nil || result.txStatus == nil || result.ctrl == nil {
return nil, fmt.Errorf("AkiraOS app-transfer service not found on device %s (is the device advertising and in range?)", prefix)
}
return result, nil
}

// ---- DBus notification watcher ----------------------------------------------

func watchNotify(conn *dbus.Conn, charPath dbus.ObjectPath) (_ <-chan []byte, cancel func(), err error) {
matchOpts := []dbus.MatchOption{
dbus.WithMatchObjectPath(charPath),
dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
dbus.WithMatchMember("PropertiesChanged"),
}
if err := conn.AddMatchSignal(matchOpts...); err != nil {
return nil, nil, fmt.Errorf("AddMatchSignal: %w", err)
}

sigCh := make(chan *dbus.Signal, 32)
conn.Signal(sigCh)

done := make(chan struct{})
valCh := make(chan []byte, 16)

go func() {
defer close(valCh)
for {
select {
case <-done:
return
case sig, ok := <-sigCh:
if !ok {
return
}
if len(sig.Body) < 2 {
continue
}
changed, ok := sig.Body[1].(map[string]dbus.Variant)
if !ok {
continue
}
if v, ok := changed["Value"]; ok {
if b, ok := v.Value().([]byte); ok {
select {
case valCh <- b:
default: // drop if consumer is too slow
}
}
}
}
}
}()

return valCh, func() {
close(done)
conn.RemoveSignal(sigCh)
_ = conn.RemoveMatchSignal(matchOpts...)
}, nil
}

// ---- Status wait ------------------------------------------------------------

// waitForStatus blocks until pred signals done or ctx expires.
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
