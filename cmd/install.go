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

var installDevice string
var installToken string
var installTimeout int
 
 

var installCmd = &cobra.Command{
	Use:   "install <app.akpkg>",
	Short: "Push a signed .akpkg to an AkiraOS device over WiFi",
	Long: `Upload a signed .akpkg to a running AkiraOS device via its HTTP OTA endpoint.

The device must be reachable at the given IP address and the bearer token must
match CONFIG_AKIRA_OTA_TOKEN in the device's prj.conf.

	akira-cli install hello.akpkg --device 192.168.1.42 --token my-secret

The firmware validates the Ed25519 signature before committing the install.
Use 'akira-cli sign' first if the package is not yet signed.
`,
	Args: cobra.ExactArgs(1),
	RunE: runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installDevice, "device", "", "device IP address or hostname (required)")
	installCmd.Flags().StringVar(&installToken, "token", "", "OTA bearer token (required)")
	installCmd.Flags().IntVar(&installTimeout, "timeout", 30, "HTTP timeout in seconds")
	_ = installCmd.MarkFlagRequired("device")
	_ = installCmd.MarkFlagRequired("token")
	rootCmd.AddCommand(installCmd)
}

func runInstall(_ *cobra.Command, args []string) error {
	pkgPath := args[0]

	client := transport.NewHTTPClient(installDevice, installToken, installTimeout)

	fmt.Printf("Installing %s → %s …\n", pkgPath, installDevice)
	resp, err := client.Install(pkgPath)
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}

	fmt.Printf("OK  %s installed (device response: %s)\n", pkgPath, resp)
	return nil
}


