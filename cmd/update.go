/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/PenEngineering/akira-cli/internal/transport"
	"github.com/spf13/cobra"
)

var updateDevice  string
var updateToken   string
var updateTimeout int

var updateCmd = &cobra.Command{
	Use:   "update <zephyr.signed.bin>",
	Short: "Flash new AkiraOS firmware via OTA (HTTP /upload)",
	Long: `Upload a signed firmware image to the AkiraOS device's /upload endpoint.

The file must be zephyr.signed.bin (NOT zephyr.bin) produced by the build:

  build-akiraconsole/zephyr/zephyr.signed.bin

The device streams the image directly to the secondary MCUboot slot, validates
the MCUboot image header, then marks the slot for next-boot upgrade.
After a successful upload, reboot the device to apply the update.

Examples:

  # Device with auth disabled (CONFIG_AKIRA_HTTP_NO_AUTH=y)
  akira-cli update zephyr.signed.bin --device 192.168.1.42

  # Device with auth enabled
  akira-cli update zephyr.signed.bin --device 192.168.1.42 --token my-secret

  # After upload, reboot via shell or wait for automatic reboot
`,
	Args: cobra.ExactArgs(1),
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().StringVar(&updateDevice,  "device",  "", "device IP or hostname (required)")
	updateCmd.Flags().StringVar(&updateToken,   "token",   "", "OTA bearer token (only needed when device auth is enabled)")
	updateCmd.Flags().IntVar(&updateTimeout,    "timeout", 30, "initial HTTP timeout in seconds (upload adds 120 s automatically)")
	_ = updateCmd.MarkFlagRequired("device")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(_ *cobra.Command, args []string) error {
	firmwarePath := args[0]

	// Validate the file exists before connecting to the device.
	info, err := os.Stat(firmwarePath)
	if err != nil {
		return fmt.Errorf("firmware file: %w", err)
	}

	base := filepath.Base(firmwarePath)
	if base == "zephyr.bin" {
		fmt.Fprintf(os.Stderr,
			"warning: you supplied zephyr.bin — OTA requires zephyr.signed.bin\n"+
				"         (MCUboot image header must be present)\n")
	}

	fmt.Printf("Uploading %s (%.1f KB) → %s …\n",
		base, float64(info.Size())/1024.0, updateDevice)

	client := transport.NewHTTPClient(updateDevice, updateToken, updateTimeout)

	var lastPct int64 = -1
	err = client.OtaUpdate(firmwarePath, func(read, total int64) {
		if total <= 0 {
			return
		}
		pct := read * 100 / total
		if pct != lastPct && pct%10 == 0 {
			fmt.Printf("  %3d%%  (%d / %d bytes)\n", pct, read, total)
			lastPct = pct
		}
	})
	if err != nil {
		return fmt.Errorf("OTA update: %w", err)
	}

	fmt.Println("Upload complete — reboot the device to apply the firmware.")
	return nil
}
