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
var keygenPQC bool

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate Ed25519 keypair and device provisioning bundle",
	Long: `Generate a fresh Ed25519 keypair and write three files:

  privkey.pem          — PKCS#8 PEM private key (keep secret, chmod 600)
  pubkey.pem           — PKIX PEM public key
  device_provision.txt — pubkey hex ready to paste into prj.conf

With --pqc, also generates a Dilithium-2 (FIPS 204) keypair:

  dilithium2_privkey.pem — Dilithium-2 private key (keep secret, chmod 600)
  dilithium2_pubkey.pem  — Dilithium-2 public key
  pqc_provision.txt      — pubkey hex for CONFIG_AKIRA_PLATFORM_PQC_PUBKEY

The --pqc flag requires AkiraPlatform (CONFIG_AKIRA_PLATFORM_PQC_SIGNING=y).`,
	RunE: runKeygen,
}

func init() {
	keygenCmd.Flags().StringVarP(&keygenOut, "out", "o", ".", "directory to write key files into")
	keygenCmd.Flags().BoolVar(&keygenPQC, "pqc", false, "also generate Dilithium-2 keypair (AkiraPlatform)")
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

	fmt.Printf("Generated Ed25519 keypair:\n")
	fmt.Printf("  %s/privkey.pem          (keep secret)\n", keygenOut)
	fmt.Printf("  %s/pubkey.pem\n", keygenOut)
	fmt.Printf("  %s/device_provision.txt  (paste into prj.conf)\n", keygenOut)

	if keygenPQC {
		pqcPub, pqcPriv, err := crypto.GenerateDilithiumKeyPair()
		if err != nil {
			return fmt.Errorf("dilithium key generation: %w", err)
		}
		if err := crypto.WriteDilithiumPrivateKey(pqcPriv, keygenOut+"/dilithium2_privkey.pem"); err != nil {
			return fmt.Errorf("write dilithium2_privkey.pem: %w", err)
		}
		if err := crypto.WriteDilithiumPublicKey(pqcPub, keygenOut+"/dilithium2_pubkey.pem"); err != nil {
			return fmt.Errorf("write dilithium2_pubkey.pem: %w", err)
		}
		if err := crypto.WriteDilithiumProvisionBundle(pqcPub, keygenOut+"/pqc_provision.txt"); err != nil {
			return fmt.Errorf("write pqc_provision.txt: %w", err)
		}
		fmt.Printf("\nGenerated Dilithium-2 keypair (AkiraPlatform PQC):\n")
		fmt.Printf("  %s/dilithium2_privkey.pem  (keep secret)\n", keygenOut)
		fmt.Printf("  %s/dilithium2_pubkey.pem\n", keygenOut)
		fmt.Printf("  %s/pqc_provision.txt        (paste into prj.conf)\n", keygenOut)
	}
	return nil
}
