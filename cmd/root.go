/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

// Package cmd implements the akira-cli command tree.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "akira-cli",
	Short: "AkiraOS developer toolchain",
	Long: `akira-cli — package, sign, verify, and deploy WASM applications to AkiraOS devices.

Commands:
  init     Install AkiraOS development dependencies (WASI SDK, WAMR, wamrc)
  keygen   Generate an Ed25519 keypair and device provisioning bundle
  pack     Bundle a WASM app + manifest into an .akpkg archive
  sign     Sign an .akpkg with an Ed25519 private key
  verify   Verify an .akpkg signature offline
  install  Push a signed .akpkg to an AkiraOS device over WiFi
  update   Flash new AkiraOS firmware via OTA (HTTP /upload)
  info     Print a connected device's info (/api/v1/info)
  logs     Fetch a connected device's logs (/api/v1/logs)
  apps     List / start / stop / remove apps on a device

The app developer workflow (new, build, deploy --sim, device discovery and
publish) lives in the Python 'akira' CLI (pipx install akira-dev).`,
	SilenceUsage: true,
}

// Execute is the entry point called by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
