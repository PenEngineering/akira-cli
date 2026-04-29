/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

package cmd

import (
	"fmt"

	"github.com/PenEngineering/akira-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var keygenOut string

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate Ed25519 keypair and device provisioning bundle",
	Long: `Generate a fresh Ed25519 keypair and write three files:

  privkey.pem          — PKCS#8 PEM private key (keep secret, chmod 600)
  pubkey.pem           — PKIX PEM public key
  device_provision.txt — pubkey hex ready to paste into prj.conf

The hex value in device_provision.txt is the raw 32-byte public key encoded
as CONFIG_AKIRA_APP_PUBKEY in your board's prj.conf so the firmware can
verify signed .akpkg files at install time.`,
	RunE: runKeygen,
}

func init() {
	keygenCmd.Flags().StringVarP(&keygenOut, "out", "o", ".", "directory to write key files into")
	rootCmd.AddCommand(keygenCmd)
}

func runKeygen(_ *cobra.Command, _ []string) error {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("key generation: %w", err)
	}

	if err := crypto.WritePrivateKey(priv, keygenOut+"/privkey.pem"); err != nil {
		return fmt.Errorf("write privkey.pem: %w", err)
	}
	if err := crypto.WritePublicKey(pub, keygenOut+"/pubkey.pem"); err != nil {
		return fmt.Errorf("write pubkey.pem: %w", err)
	}
	if err := crypto.WriteProvisionBundle(pub, keygenOut+"/device_provision.txt"); err != nil {
		return fmt.Errorf("write device_provision.txt: %w", err)
	}

	fmt.Printf("Generated keypair:\n")
	fmt.Printf("  %s/privkey.pem          (keep secret)\n", keygenOut)
	fmt.Printf("  %s/pubkey.pem\n", keygenOut)
	fmt.Printf("  %s/device_provision.txt  (paste into prj.conf)\n", keygenOut)
	return nil
}
