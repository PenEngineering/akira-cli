/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

package cmd

import (
	"fmt"

	"github.com/PenEngineering/akira-cli/internal/akpkg"
	"github.com/PenEngineering/akira-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var verifyPubKey string
var verifyPQC bool
var verifyPQCPubKey string

var verifyCmd = &cobra.Command{
	Use:   "verify <app.akpkg>",
	Short: "Verify an .akpkg signature offline",
	Long: `Verify the Ed25519 signature embedded in an .akpkg archive.

The command extracts manifest.json and app.wasm, recomputes
SHA-256(manifest_bytes || wasm_bytes), and checks the sig.ed25519 entry
against the supplied public key. No network access required.

With --pqc and --pqc-pubkey, also verifies the sig.dilithium2 entry against
the Dilithium-2 public key (AkiraPlatform feature).`,
	Args: cobra.ExactArgs(1),
	RunE: runVerify,
}

func init() {
	verifyCmd.Flags().StringVar(&verifyPubKey, "pubkey", "", "path to pubkey.pem (required)")
	verifyCmd.Flags().BoolVar(&verifyPQC, "pqc", false, "also verify Dilithium-2 signature (AkiraPlatform)")
	verifyCmd.Flags().StringVar(&verifyPQCPubKey, "pqc-pubkey", "", "path to dilithium2_pubkey.pem (required with --pqc)")
	_ = verifyCmd.MarkFlagRequired("pubkey")
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(_ *cobra.Command, args []string) error {
	pkgPath := args[0]

	if verifyPQC && verifyPQCPubKey == "" {
		return fmt.Errorf("--pqc-pubkey is required when --pqc is set")
	}

	pub, err := crypto.LoadPublicKey(verifyPubKey)
	if err != nil {
		return fmt.Errorf("load pubkey: %w", err)
	}

	var dilithiumPub []byte
	if verifyPQC {
		dilithiumPub, err = crypto.LoadDilithiumPublicKey(verifyPQCPubKey)
		if err != nil {
			return fmt.Errorf("load pqc-pubkey: %w", err)
		}
	}

	info, err := akpkg.Verify(pkgPath, pub, dilithiumPub)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	fmt.Printf("OK  %s\n", pkgPath)
	fmt.Printf("    app:      %s  v%s\n", info.Name, info.Version)
	fmt.Printf("    wasm:     %d bytes\n", info.WASMSize)
	fmt.Printf("    manifest: %d bytes\n", info.ManifestSize)
	if verifyPQC {
		fmt.Printf("    PQC sig:  Dilithium-2 OK\n")
	}
	return nil
}
