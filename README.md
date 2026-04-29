# akira-cli

Developer toolchain for [AkiraOS](https://github.com/PenEngineering/AkiraOS) — package, sign, verify, and deploy WASM apps to AkiraOS devices.

## Installation

```sh
# Homebrew (macOS / Linux)
brew tap akiraos/tap
brew install akira-cli

# Go install
go install github.com/PenEngineering/akira-cli@latest

# Download binary from GitHub Releases
# https://github.com/PenEngineering/akira-cli/releases
```

## Commands

### `keygen` — Generate keypair + provisioning bundle

```sh
akira-cli keygen --out ./keys
```

Writes `privkey.pem`, `pubkey.pem`, and `device_provision.txt` containing the `CONFIG_AKIRA_APP_PUBKEY` line for your board's `prj.conf`.

---

### `pack` — Bundle WASM + manifest into `.akpkg`

```sh
akira-cli pack hello.wasm manifest.json
# → hello.akpkg

akira-cli pack hello.wasm manifest.json --out dist/hello.akpkg
```

---

### `sign` — Attach Ed25519 signature

```sh
akira-cli sign hello.akpkg --key ./keys/privkey.pem
# overwrites hello.akpkg with a signed copy

akira-cli sign hello.akpkg --key ./keys/privkey.pem --out hello-signed.akpkg
```

The signature covers `SHA-256(manifest_bytes || wasm_bytes)`.

---

### `verify` — Verify signature offline

```sh
akira-cli verify hello.akpkg --pubkey ./keys/pubkey.pem
```

---

### `install` — Push to device over WiFi

```sh
akira-cli install hello.akpkg --device 192.168.1.42 --token my-ota-secret
```

POSTs the `.akpkg` to `http://<device>/api/apps/install` with a `Bearer` token. The firmware validates the signature before committing.

---

## `.akpkg` Format

A `.akpkg` is a gzip-compressed tar archive:

| Entry          | Required | Description                          |
|----------------|----------|--------------------------------------|
| `manifest.json`| yes      | Application metadata (name, version, permissions) |
| `app.wasm`     | yes      | Compiled WebAssembly binary          |
| `sig.ed25519`  | no       | 64-byte Ed25519 signature (added by `sign`) |

## License

Apache 2.0 — see [LICENSE](LICENSE).
