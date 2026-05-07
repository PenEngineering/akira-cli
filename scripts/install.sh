#!/usr/bin/env bash
set -euo pipefail

REPO="PenEngineering/akira-cli"

detect_platform() {
  uname_s=$(uname -s)
  case "$uname_s" in
    Linux) OS=linux ;; 
    Darwin) OS=darwin ;;
    *) echo "Unsupported OS: $uname_s" >&2; exit 1 ;;
  esac

  uname_m=$(uname -m)
  case "$uname_m" in
    x86_64|amd64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
    *) echo "Unsupported arch: $uname_m" >&2; exit 1 ;;
  esac
}

download_and_install() {
  BASENAME="akira-cli_${OS}_${ARCH}"
  URL="https://github.com/${REPO}/releases/latest/download/${BASENAME}"

  tmpdir=$(mktemp -d)
  out="$tmpdir/$BASENAME"

  echo "Downloading ${URL}..."
  curl -fL "$URL" -o "$out"
  chmod +x "$out"

  dest="/usr/local/bin/akira-cli"
  if [ -w "$(dirname "$dest")" ]; then
    mv "$out" "$dest"
  else
    echo "Installing to $dest (sudo may be required)"
    sudo mv "$out" "$dest"
  fi
  echo "Installed to $dest"
  rm -rf "$tmpdir"
}

detect_platform
download_and_install
