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

var signKey string
var signOut string

var signCmd = &cobra.Command{
	Use:   "sign <app.akpkg>",
	Short: "Sign an .akpkg with an Ed25519 private key",
	Long: `Attach an Ed25519 signature to an .akpkg archive.

The signature is computed over SHA-256(manifest_bytes || wasm_bytes) and stored
as 'sig.ed25519' (64 bytes) inside the archive. The device firmware validates
this signature at install time using the public key from CONFIG_AKIRA_APP_PUBKEY.

A signed package replaces (or is written alongside) the original — use --out to
control the output path.`,
	Args: cobra.ExactArgs(1),
	RunE: runSign,
}

func init() {
	signCmd.Flags().StringVar(&signKey, "key", "", "path to privkey.pem (required)")
	signCmd.Flags().StringVarP(&signOut, "out", "o", "", "output path (default: overwrites input)")
	_ = signCmd.MarkFlagRequired("key")
	rootCmd.AddCommand(signCmd)
}

func runSign(_ *cobra.Command, args []string) error {
	pkgPath := args[0]
	out := signOut
	if out == "" {
		out = pkgPath
	}

	priv, err := crypto.LoadPrivateKey(signKey)
	if err != nil {
		return fmt.Errorf("load key: %w", err)
	}

	if err := akpkg.Sign(pkgPath, priv, out); err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	fmt.Printf("Signed → %s\n", out)
	return nil
}
