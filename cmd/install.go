/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

package cmd

import (
	"fmt"

	"github.com/PenEngineering/akira-cli/internal/transport"
	"github.com/spf13/cobra"
)

var installDevice    string
var installToken     string
var installTimeout   int
var installTransport string
var installBTAdapter string

var installCmd = &cobra.Command{
	Use:   "install <app.akpkg>",
	Short: "Push a signed .akpkg app to an AkiraOS device",
	Long: `Upload a signed .akpkg app to a running AkiraOS device.

Three transports are supported:

  WiFi / HTTP (default):
    The device must be reachable at the given IP address and the bearer token
    must match CONFIG_AKIRA_OTA_TOKEN in the device's prj.conf.

      akira-cli install hello.akpkg --device 192.168.1.42:8080 --token my-secret

  Bluetooth LE (GATT):
    The device must be advertising the AkiraOS App Transfer Service and within
    BLE range. --token is not used.

      akira-cli install hello.akpkg --transport bt --device AA:BB:CC:DD:EE:FF

  USB HID:
    The device must be connected via USB. No --device flag needed.

      akira-cli install hello.akpkg --transport usb

The device validates the Ed25519 signature before committing the app.
Use 'akira-cli sign' first if the package is not yet signed.
`,
	Args: cobra.ExactArgs(1),
	RunE: runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installDevice,    "device",      "", "device IP/hostname (HTTP) or BT MAC address (BLE) (required)")
	installCmd.Flags().StringVar(&installToken,     "token",       "", "OTA bearer token (HTTP transport only)")
	installCmd.Flags().IntVar(&installTimeout,      "timeout",     30, "transport timeout in seconds")
	installCmd.Flags().StringVar(&installTransport, "transport",   "http", `transport protocol: "http", "bt", or "usb"`)
	installCmd.Flags().StringVar(&installBTAdapter, "bt-adapter",  "hci0", "local Bluetooth adapter (BLE transport only)")
	rootCmd.AddCommand(installCmd)
}

func runInstall(_ *cobra.Command, args []string) error {
	pkgPath := args[0]

	switch installTransport {
	case "http":
		if installDevice == "" {
			return fmt.Errorf("--device is required for HTTP transport")
		}
		if installToken == "" {
			return fmt.Errorf("--token is required for HTTP transport")
		}
		client := transport.NewHTTPClient(installDevice, installToken, installTimeout)
		fmt.Printf("Installing %s → %s (HTTP) …\n", pkgPath, installDevice)
		resp, err := client.Install(pkgPath)
		if err != nil {
			return fmt.Errorf("install: %w", err)
		}
		fmt.Printf("OK  %s installed (device response: %s)\n", pkgPath, resp)

	case "bt":
		if installDevice == "" {
			return fmt.Errorf("--device is required for BLE transport")
		}
		client := transport.NewBTClient(installDevice, installBTAdapter, installTimeout)
		fmt.Printf("Installing %s → %s (BLE GATT) …\n", pkgPath, installDevice)
		resp, err := client.Install(pkgPath)
		if err != nil {
			return fmt.Errorf("install: %w", err)
		}
		fmt.Printf("OK  %s installed (%s)\n", pkgPath, resp)

	case "usb":
		client := transport.NewUSBClient(installTimeout)
		fmt.Printf("Installing %s → USB HID (VID=0x2FE3 PID=0x0001) …\n", pkgPath)
		resp, err := client.Install(pkgPath)
		if err != nil {
			return fmt.Errorf("install: %w", err)
		}
		fmt.Printf("OK  %s installed (%s)\n", pkgPath, resp)

	default:
		return fmt.Errorf("unknown transport %q; use \"http\", \"bt\", or \"usb\"", installTransport)
	}

	return nil
}


