#!/usr/bin/env bash
# gopath-setup.sh — add Go's bin directory to PATH in the current shell profile.
# Run once:  bash scripts/gopath-setup.sh
# Then restart your shell or run:  source ~/.bashrc  (or ~/.zshrc)

set -euo pipefail

GOBIN="$(go env GOPATH)/bin"

if [ -z "$GOBIN" ]; then
  echo "Error: 'go' is not installed or GOPATH is empty." >&2
  echo "Install Go from https://go.dev/dl/ then re-run this script." >&2
  exit 1
fi

# Detect the active shell profile file.
PROFILE=""
case "${SHELL:-}" in
  */zsh)  PROFILE="$HOME/.zshrc" ;;
  */bash) PROFILE="$HOME/.bashrc" ;;
  */fish) PROFILE="$HOME/.config/fish/config.fish" ;;
  *)
    # Fallback: try common files in order.
    for f in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
      [ -f "$f" ] && PROFILE="$f" && break
    done
    ;;
esac

if [ -z "$PROFILE" ]; then
  echo "Could not detect shell profile. Add the following line manually:" >&2
  echo "  export PATH=\"${GOBIN}:\$PATH\"" >&2
  exit 1
fi

EXPORT_LINE="export PATH=\"${GOBIN}:\$PATH\""

if grep -qF "$GOBIN" "$PROFILE" 2>/dev/null; then
  echo "GOBIN is already in $PROFILE — nothing to do."
else
  printf '\n# Added by akira-cli gopath-setup.sh\n%s\n' "$EXPORT_LINE" >> "$PROFILE"
  echo "Added GOBIN to $PROFILE"
fi

echo ""
echo "GOBIN: $GOBIN"
echo "To apply now, run:"
echo "  source $PROFILE"
