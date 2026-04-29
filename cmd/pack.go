/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

package cmd

import (
	"fmt"
	"strings"

	"github.com/PenEngineering/akira-cli/internal/akpkg"
	"github.com/spf13/cobra"
)

var packOut string

var packCmd = &cobra.Command{
	Use:   "pack <app.wasm> <manifest.json>",
	Short: "Bundle a WASM app and manifest into an .akpkg archive",
	Long: `Create an unsigned .akpkg archive from a compiled WASM binary and a manifest.

The .akpkg format is a gzip-compressed tar containing:
  manifest.json   — application metadata (name, version, permissions, …)
  app.wasm        — compiled WebAssembly binary

Use 'akira-cli sign' to attach an Ed25519 signature before deploying.`,
	Args: cobra.ExactArgs(2),
	RunE: runPack,
}

func init() {
	packCmd.Flags().StringVarP(&packOut, "out", "o", "", "output .akpkg path (default: <app_name>.akpkg)")
	rootCmd.AddCommand(packCmd)
}

func runPack(_ *cobra.Command, args []string) error {
	wasmPath := args[0]
	manifestPath := args[1]

	out := packOut
	if out == "" {
		// derive from wasm filename: foo.wasm → foo.akpkg
		base := wasmPath
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		out = strings.TrimSuffix(base, ".wasm") + ".akpkg"
	}

	if err := akpkg.Pack(wasmPath, manifestPath, out); err != nil {
		return fmt.Errorf("pack: %w", err)
	}

	fmt.Printf("Packed → %s\n", out)
	return nil
}
