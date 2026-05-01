/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	wasiSDKVersion  = "25"
	wasiSDKBase     = "https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-%s"
	wamrRepo        = "https://github.com/ArturR0k3r/wasm-micro-runtime.git"
	wamrBranch      = "AkiraOS_Patch"
	espressifLLVM   = "https://github.com/espressif/llvm-project.git"
	llvmBranch      = "xtensa_release_18.1.2"
)

var initWASIDir  string
var initWAMRDir  string
var initSkipWASI bool
var initSkipWAMR bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Install AkiraOS development dependencies (WASI SDK, WAMR, wamrc)",
	Long: `Bootstrap the AkiraOS WASM toolchain on this machine.

Installs:
  1. WASI SDK ` + wasiSDKVersion + `   — C→WASM compiler (clang + wasi-libc)
  2. WAMR (AkiraOS_Patch branch) — runtime + wamrc AOT compiler
     Builds wamrc with Espressif LLVM (Xtensa backend) for ESP32-S3 support.

Prerequisites (must already be installed):
  cmake, ninja (or make), python3, git, curl/wget

  akira-cli init
  akira-cli init --wasi-dir /opt/wasi-sdk --wamr-dir ~/wamr
  akira-cli init --skip-wasi   # only install wamrc
  akira-cli init --skip-wamr   # only install WASI SDK`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initWASIDir, "wasi-dir", "/opt/wasi-sdk", "install WASI SDK to this directory")
	initCmd.Flags().StringVar(&initWAMRDir, "wamr-dir", filepath.Join(os.Getenv("HOME"), "wamr"), "clone WAMR into this directory")
	initCmd.Flags().BoolVar(&initSkipWASI, "skip-wasi", false, "skip WASI SDK installation")
	initCmd.Flags().BoolVar(&initSkipWAMR, "skip-wamr", false, "skip WAMR / wamrc build")
	rootCmd.AddCommand(initCmd)
}

func runInit(_ *cobra.Command, _ []string) error {
	checkPrereqs()

	if !initSkipWASI {
		if err := installWASI(); err != nil {
			return err
		}
	} else {
		fmt.Println("Skipping WASI SDK.")
	}

	if !initSkipWAMR {
		if err := installWAMR(); err != nil {
			return err
		}
	} else {
		fmt.Println("Skipping WAMR / wamrc.")
	}

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Println("Setup complete. Add these to your shell profile:")
	fmt.Println()
	fmt.Printf("  export WASI_SDK=%s\n", initWASIDir)
	wamrcBin := filepath.Join(initWAMRDir, "wamr-compiler", "build", "wamrc")
	fmt.Printf("  export PATH=\"$PATH:%s\"\n", filepath.Dir(wamrcBin))
	fmt.Println()
	fmt.Println("Then build an app:")
	fmt.Println("  cd wasm_apps && make && make aot-xtensa")
	fmt.Println("  akira-cli pack bin/hello_world-xtensa.aot console_apps/hello_world/manifest.json")
	fmt.Println("  akira-cli sign hello_world.akpkg --key ./keys/privkey.pem")
	fmt.Println("  akira-cli install hello_world.akpkg --device <ip> --token <token>")
	fmt.Println("─────────────────────────────────────────────────")
	return nil
}

// checkPrereqs warns about missing system tools but does not abort.
func checkPrereqs() {
	tools := []string{"git", "cmake", "python3", "curl"}
	missing := []string{}
	for _, t := range tools {
		if _, err := exec.LookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		fmt.Printf("WARNING: missing tools: %s\n", strings.Join(missing, ", "))
		fmt.Println("Install them with your package manager before continuing.")
		fmt.Println()
	}
}

// installWASI downloads and extracts the WASI SDK for the current platform.
func installWASI() error {
	// Check if already installed.
	clang := filepath.Join(initWASIDir, "bin", "clang")
	if _, err := os.Stat(clang); err == nil {
		ver, _ := runOutput(clang, "--version")
		fmt.Printf("WASI SDK already present at %s (%s)\n", initWASIDir, firstLine(ver))
		return nil
	}

	tarball, err := wasiSDKTarball()
	if err != nil {
		return err
	}
	url := fmt.Sprintf(wasiSDKBase+"/"+tarball, wasiSDKVersion, wasiSDKVersion)

	fmt.Printf("Downloading WASI SDK %s …\n", wasiSDKVersion)
	fmt.Printf("  %s\n", url)

	tmp := filepath.Join(os.TempDir(), tarball)
	if err := downloadFile(url, tmp); err != nil {
		return fmt.Errorf("download WASI SDK: %w", err)
	}
	defer os.Remove(tmp)

	fmt.Printf("Extracting to %s …\n", initWASIDir)
	if err := os.MkdirAll(filepath.Dir(initWASIDir), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// tar extracts a versioned subdirectory; move it into place.
	extractDir := filepath.Dir(initWASIDir)
	if err := runCmd("tar", "xf", tmp, "-C", extractDir); err != nil {
		return fmt.Errorf("extract WASI SDK: %w", err)
	}
	// Rename e.g. wasi-sdk-25.0-x86_64-linux → initWASIDir if different.
	extracted := strings.TrimSuffix(tarball, ".tar.gz")
	extracted = strings.TrimSuffix(extracted, ".tar.bz2")
	srcDir := filepath.Join(extractDir, extracted)
	if srcDir != initWASIDir {
		if err := os.Rename(srcDir, initWASIDir); err != nil {
			fmt.Printf("NOTE: extracted to %s (rename to %s failed: %v)\n", srcDir, initWASIDir, err)
			fmt.Printf("Set WASI_SDK=%s manually.\n", srcDir)
		}
	}

	fmt.Println("✓ WASI SDK installed.")
	return nil
}

// wasiSDKTarball returns the tarball filename for the current OS/arch.
func wasiSDKTarball() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	archMap := map[string]string{
		"amd64": "x86_64",
		"arm64": "arm64",
	}
	arch, ok := archMap[goarch]
	if !ok {
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}

	var osStr string
	switch goos {
	case "linux":
		osStr = "linux"
	case "darwin":
		osStr = "macos"
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}

	// e.g. wasi-sdk-25.0-x86_64-linux.tar.gz
	return fmt.Sprintf("wasi-sdk-%s.0-%s-%s.tar.gz", wasiSDKVersion, arch, osStr), nil
}

// installWAMR clones the AkiraOS WAMR fork and builds wamrc with Espressif LLVM.
func installWAMR() error {
	wamrcBin := filepath.Join(initWAMRDir, "wamr-compiler", "build", "wamrc")
	if _, err := os.Stat(wamrcBin); err == nil {
		fmt.Printf("wamrc already built at %s\n", wamrcBin)
		return nil
	}

	// Clone WAMR if not present.
	if _, err := os.Stat(filepath.Join(initWAMRDir, ".git")); os.IsNotExist(err) {
		fmt.Printf("Cloning WAMR (%s branch) …\n", wamrBranch)
		if err := runCmd("git", "clone", "--branch", wamrBranch, "--depth=1", wamrRepo, initWAMRDir); err != nil {
			return fmt.Errorf("clone WAMR: %w", err)
		}
	} else {
		fmt.Printf("WAMR already cloned at %s\n", initWAMRDir)
	}

	// Build Espressif LLVM (Xtensa backend) — required for ESP32-S3 AOT.
	fmt.Println("Building Espressif LLVM (Xtensa backend) — this takes 10–30 minutes …")
	buildLLVM := filepath.Join(initWAMRDir, "build-scripts", "build_llvm.py")
	if err := runCmdDir(initWAMRDir, "python3", buildLLVM, "--platform", "xtensa", "--arch", "Xtensa"); err != nil {
		return fmt.Errorf("build LLVM: %w", err)
	}

	// Build wamrc.
	fmt.Println("Building wamrc …")
	buildDir := filepath.Join(initWAMRDir, "wamr-compiler", "build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("mkdir wamrc build: %w", err)
	}
	if err := runCmdDir(buildDir, "cmake", ".."); err != nil {
		return fmt.Errorf("cmake wamrc: %w", err)
	}
	nproc := fmt.Sprintf("-j%d", runtime.NumCPU())
	if err := runCmdDir(buildDir, "make", nproc); err != nil {
		return fmt.Errorf("make wamrc: %w", err)
	}

	fmt.Printf("✓ wamrc built at %s\n", wamrcBin)
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func runCmd(name string, args ...string) error {
	return runCmdDir("", name, args...)
}

func runCmdDir(dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func runOutput(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func downloadFile(url, dest string) error {
	// Prefer curl; fall back to wget.
	if _, err := exec.LookPath("curl"); err == nil {
		return runCmd("curl", "-fsSL", "-o", dest, url)
	}
	if _, err := exec.LookPath("wget"); err == nil {
		return runCmd("wget", "-q", "-O", dest, url)
	}
	return fmt.Errorf("neither curl nor wget found — install one and retry")
}
