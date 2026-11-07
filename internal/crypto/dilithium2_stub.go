/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

//go:build !cgo || !pqc

// Fallback when akira-cli is built without the `pqc` tag (or without cgo). The real
// Dilithium-2 implementation (dilithium2.go) needs liboqs via cgo; without it,
// PQC signing/verification is unavailable. These stubs keep the rest of the tool
// (pack, sign, verify, install, the device commands) building and working — the
// PQC paths are only reached when the user passes Dilithium flags, and then fail
// with a clear message instead of a link error.
package crypto

import "fmt"

const dilithiumUnavailable = "Dilithium-2 (PQC) is unavailable: this akira-cli was built " +
	"without the pqc build tag. Rebuild with CGO_ENABLED=1 and -tags pqc (liboqs required)."

// GenerateDilithiumKeyPair is unavailable in a cgo-free build.
func GenerateDilithiumKeyPair() (pub, priv []byte, err error) {
	return nil, nil, fmt.Errorf(dilithiumUnavailable)
}

// WriteDilithiumPrivateKey is unavailable in a cgo-free build.
func WriteDilithiumPrivateKey(priv []byte, path string) error {
	return fmt.Errorf(dilithiumUnavailable)
}

// WriteDilithiumPublicKey is unavailable in a cgo-free build.
func WriteDilithiumPublicKey(pub []byte, path string) error {
	return fmt.Errorf(dilithiumUnavailable)
}

// WriteDilithiumProvisionBundle is unavailable in a cgo-free build.
func WriteDilithiumProvisionBundle(pub []byte, path string) error {
	return fmt.Errorf(dilithiumUnavailable)
}

// LoadDilithiumPrivateKey is unavailable in a cgo-free build.
func LoadDilithiumPrivateKey(path string) ([]byte, error) {
	return nil, fmt.Errorf(dilithiumUnavailable)
}

// LoadDilithiumPublicKey is unavailable in a cgo-free build.
func LoadDilithiumPublicKey(path string) ([]byte, error) {
	return nil, fmt.Errorf(dilithiumUnavailable)
}

// SignDilithium is unavailable in a cgo-free build.
func SignDilithium(priv, message []byte) ([]byte, error) {
	return nil, fmt.Errorf(dilithiumUnavailable)
}

// VerifyDilithium is unavailable in a cgo-free build.
func VerifyDilithium(pub, message, signature []byte) bool {
	return false
}
