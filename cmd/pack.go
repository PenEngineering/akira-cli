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

var packOut  string
var packIcon string

var packCmd = &cobra.Command{
	Use:   "pack <app.wasm|app.aot> <manifest.json>",
	Short: "Bundle a WASM or AOT app and manifest into an .akpkg archive",
	Long: `Create an unsigned .akpkg archive from a compiled binary and a manifest.

The binary format is detected via magic bytes (not the file extension):
  \0asm  — WebAssembly bytecode
  \0aot  — WAMR AOT native binary

The .akpkg format is a gzip-compressed tar containing:
  manifest.json   — application metadata (name, version, permissions, …)
  app.wasm        — WASM or AOT binary (magic bytes identify the actual type)

Optionally embed a 32×32 app icon (PNG, JPEG, or BMP):
  akira-cli pack app.wasm manifest.json --icon icon.png

Use 'akira-cli sign' to attach an Ed25519 signature before deploying.`,
	Args: cobra.ExactArgs(2),
	RunE: runPack,
}

func init() {
	packCmd.Flags().StringVarP(&packOut,  "out",  "o", "", "output .akpkg path (default: <app_name>.akpkg)")
	packCmd.Flags().StringVar(&packIcon, "icon", "",  "path to app icon (PNG, JPEG, or BMP); resized to 32×32 RGBA and embedded in manifest")
	rootCmd.AddCommand(packCmd)
}

func runPack(_ *cobra.Command, args []string) error {
	binPath := args[0]
	manifestPath := args[1]

	out := packOut
	if out == "" {
		// derive from binary filename: foo.wasm or foo.aot → foo.akpkg
		base := binPath
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		base = strings.TrimSuffix(base, ".wasm")
		base = strings.TrimSuffix(base, ".aot")
		out = base + ".akpkg"
	}

	if packIcon != "" {
		if err := akpkg.PackWithIcon(binPath, manifestPath, packIcon, out); err != nil {
			return fmt.Errorf("pack: %w", err)
		}
	} else {
		if err := akpkg.Pack(binPath, manifestPath, out); err != nil {
			return fmt.Errorf("pack: %w", err)
		}
	}

	fmt.Printf("Packed → %s\n", out)
	return nil
}
