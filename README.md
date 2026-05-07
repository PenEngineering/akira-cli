# akira-cli

Developer toolchain for [AkiraOS](https://github.com/PenEngineering/AkiraOS) — package, sign, verify, and deploy WASM apps to AkiraOS devices.

## Installation

### Option 1 — Prebuilt binary

```sh
curl -fsSL https://raw.githubusercontent.com/PenEngineering/akira-cli/main/scripts/install.sh | bash
```

Downloads the correct binary for your OS/architecture from the latest GitHub Release and installs it to `/usr/local/bin`.

### Option 2 — Go install

```sh
go install github.com/PenEngineering/akira-cli@latest
```

Requires Go ≥ 1.22. The binary is placed in `$GOPATH/bin` (usually `~/go/bin`). Run the helper script once to add it to your `PATH`:

```sh
bash scripts/gopath-setup.sh
source ~/.bashrc   # or ~/.zshrc / ~/.config/fish/config.fish
```

> **After installing**, run `akira-cli init` once to install all required dependencies (WASI SDK, WAMR, wamrc):
> ```sh
> akira-cli init
> ```

## Commands

### `init` — Install development dependencies

```sh
akira-cli init
```

Downloads and installs the WASI SDK, WAMR runtime, and `wamrc` compiler. Must be run once before using other commands.

After `init` completes, add the following to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) and reload it:

```sh
export WASI_SDK=/opt/wasi-sdk                          # default; matches --wasi-dir if overridden
export PATH="$PATH:$HOME/wamr/wamr-compiler/build"    # exposes wamrc; matches --wamr-dir if overridden
```

```sh
source ~/.bashrc   # or ~/.zshrc / ~/.config/fish/config.fish
```

---

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

### `install` — Push to device

**Over WiFi (HTTP):**
```sh
akira-cli install hello.akpkg --device 192.168.1.42:8080 --token my-ota-secret
```

POSTs the `.akpkg` to `http://<device>/api/apps/install` with a `Bearer` token.

**Over USB HID:**
```sh
akira-cli install hello.akpkg --transport usb
```

Streams the package directly to the device over the USB HID raw channel (Report ID 3, Usage Page 0xFF60). No IP address or token required — just plug in.

> **Linux:** USB HID devices are restricted to root by default. Install the udev rule once:
> ```sh
> make udev-install
> # or manually:
> sudo cp scripts/99-akiraos.rules /etc/udev/rules.d/
> sudo udevadm control --reload-rules && sudo udevadm trigger
> sudo usermod -aG plugdev $USER   # log out and back in
> ```

> **macOS:** No extra setup needed — IOKit grants user-space access automatically.

> **Windows:** No extra setup needed — WinUSB/HID class driver is used directly.

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


