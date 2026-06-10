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
//	model.tflite    — TFLite Micro model (optional; added by PackWithModel)
//	sig.ed25519     — 64-byte Ed25519 signature (optional; added by Sign)
//
// The signature covers SHA-256(manifest_bytes || wasm_bytes [|| model_bytes]).
package akpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"time"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
)

const (
	entryManifest  = "manifest.json"
	entryWASM      = "app.wasm"
	entryAOT       = "app.aot"
	entryModel     = "model.tflite"
	entrySignature = "sig.ed25519"
)

// Binary format magic bytes.
var (
	magicWASM = []byte{0x00, 0x61, 0x73, 0x6d} // \0asm  — WebAssembly
	magicAOT  = []byte{0x00, 0x61, 0x6f, 0x74} // \0aot  — WAMR AOT
)

// Format identifies a binary's encoding.
type Format uint8

const (
	FormatUnknown Format = iota
	FormatWASM           // raw WebAssembly bytecode (\0asm)
	FormatAOT            // WAMR AOT native binary  (\0aot)
	FormatAkpkg          // gzip-compressed tar (.akpkg)
)

// DetectFormat inspects the leading bytes of data and returns the binary format.
func DetectFormat(data []byte) Format {
	if len(data) >= 4 && bytes.Equal(data[:4], magicWASM) {
		return FormatWASM
	}
	if len(data) >= 4 && bytes.Equal(data[:4], magicAOT) {
		return FormatAOT
	}
	// gzip magic: 1f 8b — .akpkg is a gzip-compressed tar.
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return FormatAkpkg
	}
	return FormatUnknown
}

// ExtractBinary unpacks an .akpkg and returns the inner WASM/AOT binary bytes.
func ExtractBinary(pkgPath string) ([]byte, error) {
	_, binary, _, _, err := readPkg(pkgPath)
	if err != nil {
		return nil, err
	}
	if binary == nil {
		return nil, fmt.Errorf("no app.wasm or app.aot entry found in %s", pkgPath)
	}
	return binary, nil
}

// Info contains metadata extracted from a verified .akpkg.
type Info struct {
	Name         string
	Version      string
	WASMSize     int64
	ManifestSize int64
}

// EmbedIcon resizes the image at iconPath to 32×32, encodes it as raw RGB565
// little-endian (2048 bytes), base64-encodes the result, and injects it as the
// "icon" field in manifestData. Supports PNG, JPEG, and BMP input.
func EmbedIcon(manifestData []byte, iconPath string) ([]byte, error) {
	f, err := os.Open(iconPath)
	if err != nil {
		return nil, fmt.Errorf("open icon: %w", err)
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode icon: %w", err)
	}

	// Resize to 32×32 using CatmullRom (high quality, same as Pillow LANCZOS).
	dst := image.NewRGBA(image.Rect(0, 0, 32, 32))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	// Convert to raw RGB565 little-endian — 2 bytes per pixel, 2048 bytes total.
	// R5 G6 B5 packed as uint16: bits [15:11]=R [10:5]=G [4:0]=B
	raw := make([]byte, 32*32*2)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			r, g, b, _ := dst.At(x, y).RGBA() // 16-bit per channel
			pixel := (uint16(r>>11) << 11) | (uint16(g>>10) << 5) | uint16(b>>11)
			binary.LittleEndian.PutUint16(raw[(y*32+x)*2:], pixel)
		}
	}

	iconB64 := base64.StdEncoding.EncodeToString(raw)

	var m map[string]interface{}
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	m["icon"] = iconB64

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return append(out, '\n'), nil
}

// PackWithIcon is like Pack but also embeds an icon image into the manifest.
func PackWithIcon(binPath, manifestPath, iconPath, outPath string) error {
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if !json.Valid(manifestData) {
		return fmt.Errorf("manifest.json is not valid JSON")
	}
	manifestData, err = EmbedIcon(manifestData, iconPath)
	if err != nil {
		return err
	}
	binData, err := os.ReadFile(binPath)
	if err != nil {
		return fmt.Errorf("read binary: %w", err)
	}
	switch DetectFormat(binData) {
	case FormatWASM, FormatAOT: // ok
	default:
		return fmt.Errorf("%s has unrecognised magic bytes — expected WASM (\\0asm) or AOT (\\0aot)", binPath)
	}
	return writePkg(outPath, manifestData, binData, nil, nil)
}

// Pack creates an unsigned .akpkg from binPath (WASM or AOT) and manifestPath,
// writing to outPath. The binary format is detected via magic bytes.
func Pack(binPath, manifestPath, outPath string) error {
	binData, err := os.ReadFile(binPath)
	if err != nil {
		return fmt.Errorf("read binary: %w", err)
	}

	// Validate binary magic — must be WASM or AOT.
	switch DetectFormat(binData) {
	case FormatWASM:
		// ok
	case FormatAOT:
		// ok
	default:
		return fmt.Errorf("%s has unrecognised magic bytes — expected WASM (\\0asm) or AOT (\\0aot)", binPath)
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if !json.Valid(manifestData) {
		return fmt.Errorf("manifest.json is not valid JSON")
	}

	return writePkg(outPath, manifestData, binData, nil, nil)
}

// PackWithModel is like Pack but also bundles a TFLite Micro model into the archive.
// The model bytes are included in the signing digest.
func PackWithModel(binPath, manifestPath, modelPath, outPath string) error {
	binData, err := os.ReadFile(binPath)
	if err != nil {
		return fmt.Errorf("read binary: %w", err)
	}
	switch DetectFormat(binData) {
	case FormatWASM, FormatAOT: // ok
	default:
		return fmt.Errorf("%s has unrecognised magic bytes — expected WASM (\\0asm) or AOT (\\0aot)", binPath)
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if !json.Valid(manifestData) {
		return fmt.Errorf("manifest.json is not valid JSON")
	}

	modelData, err := os.ReadFile(modelPath)
	if err != nil {
		return fmt.Errorf("read model: %w", err)
	}

	return writePkg(outPath, manifestData, binData, nil, modelData)
}

// Sign reads pkgPath, attaches an Ed25519 signature, and writes the result to outPath.
func Sign(pkgPath string, priv ed25519.PrivateKey, outPath string) error {
	manifest, binary, model, _, err := readPkg(pkgPath)
	if err != nil {
		return err
	}

	sig := ed25519.Sign(priv, digest(manifest, binary, model))
	return writePkg(outPath, manifest, binary, sig, model)
}

// Verify reads pkgPath, verifies the embedded signature against pub, and returns package Info.
func Verify(pkgPath string, pub ed25519.PublicKey) (*Info, error) {
	manifest, binary, model, sig, err := readPkg(pkgPath)
	if err != nil {
		return nil, err
	}
	if len(sig) == 0 {
		return nil, fmt.Errorf("package is unsigned — run 'akira-cli sign' first")
	}
	if !ed25519.Verify(pub, digest(manifest, binary, model), sig) {
		return nil, fmt.Errorf("signature mismatch")
	}

	info := &Info{
		WASMSize:     int64(len(binary)),
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

// digest returns SHA-256(manifest || wasm [|| model]) — the message signed by Sign.
// model may be nil for packages without a bundled model.
func digest(manifest, wasm, model []byte) []byte {
	h := sha256.New()
	h.Write(manifest)
	h.Write(wasm)
	if len(model) > 0 {
		h.Write(model)
	}
	return h.Sum(nil)
}

// writePkg writes a .akpkg archive to outPath. The binary entry name is chosen
// from the magic bytes: AOT magic → "app.aot", otherwise → "app.wasm".
// sig and model may be nil for unsigned or model-free packages.
func writePkg(outPath string, manifest, binary, sig, model []byte) error {
	binaryEntry := entryWASM
	if DetectFormat(binary) == FormatAOT {
		binaryEntry = entryAOT
	}
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
		{binaryEntry, binary},
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

	if len(model) > 0 {
		hdr := &tar.Header{
			Name:     entryModel,
			Size:     int64(len(model)),
			Mode:     0644,
			ModTime:  now,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(model); err != nil {
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

// readPkg reads and returns the manifest, binary (wasm or aot), model, signature, and error.
func readPkg(pkgPath string) (manifest, binary, model, sig []byte, err error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("read %s: %w", pkgPath, err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("not a valid .akpkg (gzip error): %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("read tar: %w", err)
		}

		content, err := io.ReadAll(io.LimitReader(tr, 32<<20)) // 32 MiB cap
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("read entry %s: %w", hdr.Name, err)
		}

		switch hdr.Name {
		case entryManifest:
			manifest = content
		case entryWASM, entryAOT:
			binary = content
		case entryModel:
			model = content
		case entrySignature:
			sig = content
		}
	}

	if manifest == nil {
		return nil, nil, nil, nil, fmt.Errorf("missing %s in archive", entryManifest)
	}
	if binary == nil {
		return nil, nil, nil, nil, fmt.Errorf("missing app.wasm or app.aot in archive")
	}
	return manifest, binary, model, sig, nil
}
