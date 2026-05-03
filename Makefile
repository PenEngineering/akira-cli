BINARY     := akira-cli
MODULE     := github.com/PenEngineering/akira-cli
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -s -w -X $(MODULE)/cmd.Version=$(VERSION)

PLATFORMS  := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: build install clean test release

## build: compile for the current platform
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## install: install to $GOPATH/bin (or $HOME/go/bin)
install:
	go install -ldflags "$(LDFLAGS)" .

## test: run all unit tests
test:
	go test ./...

## lint: run staticcheck (install with: go install honnef.co/go/tools/cmd/staticcheck@latest)
lint:
	staticcheck ./...

## release: cross-compile for all platforms into dist/
release: clean
	@mkdir -p dist
	@for PLATFORM in $(PLATFORMS); do \
		GOOS=$$(echo $$PLATFORM | cut -d/ -f1); \
		GOARCH=$$(echo $$PLATFORM | cut -d/ -f2); \
		OUT=dist/$(BINARY)_$${GOOS}_$${GOARCH}; \
		[ "$$GOOS" = "windows" ] && OUT="$$OUT.exe"; \
		echo "  Building $$OUT …"; \
		GOOS=$$GOOS GOARCH=$$GOARCH go build -ldflags "$(LDFLAGS)" -o "$$OUT" . || exit 1; \
	done
	@cd dist && sha256sum * > checksums.txt
	@echo "Artifacts in dist/"

## udev-install: install udev rules for AkiraOS USB HID access on Linux (requires sudo)
udev-install:
	sudo cp scripts/99-akiraos.rules /etc/udev/rules.d/
	sudo udevadm control --reload-rules && sudo udevadm trigger
	@echo "Rules installed. Run: sudo usermod -aG plugdev $$USER  (then log out/in)"

## clean: remove build artefacts
clean:
	rm -rf $(BINARY) dist/
