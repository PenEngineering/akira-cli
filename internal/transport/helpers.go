// Copyright (c) 2026 PenEngineering S.R.L
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"path/filepath"
	"strings"
)

// appNameFromPath strips the directory and .akpkg extension from a package path.
func appNameFromPath(pkgPath string) string {
	base := filepath.Base(pkgPath)
	return strings.TrimSuffix(base, ".akpkg")
}
