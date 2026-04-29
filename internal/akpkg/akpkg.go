/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

// Package akpkg implements the .akpkg archive format.
//
// An .akpkg is a gzip-compressed tar archive with the following entries:
//
//	manifest.json   — application metadata (required)
//	app.wasm        — compiled WebAssembly binary (required)
//	sig.ed25519     — 64-byte Ed25519 signature (optional; added by Sign)
//
// The signature covers SHA-256(manifest_bytes || wasm_bytes).
package akpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	entryManifest  = "manifest.json"
	entryWASM      = "app.wasm"
	entrySignature = "sig.ed25519"
)

// Info contains metadata extracted from a verified .akpkg.
type Info struct {
	Name         string
	Version      string
	WASMSize     int64
	ManifestSize int64
}

// Pack creates an unsigned .akpkg from wasmPath and manifestPath, writing to outPath.
func Pack(wasmPath, manifestPath, outPath string) error {
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("read wasm: %w", err)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Validate manifest is valid JSON.
	if !json.Valid(manifestData) {
		return fmt.Errorf("manifest.json is not valid JSON")
	}

	return writePkg(outPath, manifestData, wasmData, nil)
}

// Sign reads pkgPath, attaches an Ed25519 signature, and writes the result to outPath.
func Sign(pkgPath string, priv ed25519.PrivateKey, outPath string) error {
	manifest, wasm, _, err := readPkg(pkgPath)
	if err != nil {
		return err
	}

	sig := ed25519.Sign(priv, digest(manifest, wasm))
	return writePkg(outPath, manifest, wasm, sig)
}

// Verify reads pkgPath, verifies the embedded signature against pub, and returns package Info.
func Verify(pkgPath string, pub ed25519.PublicKey) (*Info, error) {
	manifest, wasm, sig, err := readPkg(pkgPath)
	if err != nil {
		return nil, err
	}
	if len(sig) == 0 {
		return nil, fmt.Errorf("package is unsigned — run 'akira-cli sign' first")
	}
	if !ed25519.Verify(pub, digest(manifest, wasm), sig) {
		return nil, fmt.Errorf("signature mismatch")
	}

	info := &Info{
		WASMSize:     int64(len(wasm)),
		ManifestSize: int64(len(manifest)),
	}
	// Best-effort name/version extraction from manifest JSON.
	var m struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &m); err == nil {
		info.Name = m.Name
		info.Version = m.Version
	}
	return info, nil
}

// digest returns SHA-256(manifest || wasm) — the message signed by Sign and verified by Verify.
func digest(manifest, wasm []byte) []byte {
	h := sha256.New()
	h.Write(manifest)
	h.Write(wasm)
	return h.Sum(nil)
}

// writePkg writes a .akpkg archive to outPath. sig may be nil for unsigned packages.
func writePkg(outPath string, manifest, wasm, sig []byte) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	now := time.Now()

	for _, entry := range []struct {
		name string
		data []byte
	}{
		{entryManifest, manifest},
		{entryWASM, wasm},
	} {
		hdr := &tar.Header{
			Name:     entry.name,
			Size:     int64(len(entry.data)),
			Mode:     0644,
			ModTime:  now,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(entry.data); err != nil {
			return err
		}
	}

	if sig != nil {
		hdr := &tar.Header{
			Name:     entrySignature,
			Size:     int64(len(sig)),
			Mode:     0644,
			ModTime:  now,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(sig); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// readPkg reads and returns the contents of a .akpkg archive.
func readPkg(pkgPath string) (manifest, wasm, sig []byte, err error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read %s: %w", pkgPath, err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("not a valid .akpkg (gzip error): %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read tar: %w", err)
		}

		content, err := io.ReadAll(io.LimitReader(tr, 32<<20)) // 32 MiB cap
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read entry %s: %w", hdr.Name, err)
		}

		switch hdr.Name {
		case entryManifest:
			manifest = content
		case entryWASM:
			wasm = content
		case entrySignature:
			sig = content
		}
	}

	if manifest == nil {
		return nil, nil, nil, fmt.Errorf("missing %s in archive", entryManifest)
	}
	if wasm == nil {
		return nil, nil, nil, fmt.Errorf("missing %s in archive", entryWASM)
	}
	return manifest, wasm, sig, nil
}
