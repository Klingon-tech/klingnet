.PHONY: build build-all build-qt test clean fmt vet lint release \
	build-qt-linux build-qt-darwin build-qt-macos-universal \
	build-qt-windows build-qt-windows-native build-qt-windows-docker build-qt-windows-image

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -s -w

# Build the main daemon
build:
	go build -o bin/klingnetd ./cmd/klingnetd

# Build all binaries
build-all: build build-cli build-qt

build-cli:
	go build -o bin/klingnet-cli ./cmd/klingnet-cli

# Build GUI wallet (requires wails CLI + webkit2gtk4.1-devel)
build-qt:
	cd cmd/klingnet-qt && $(HOME)/go/bin/wails build -tags webkit2_41
	cp cmd/klingnet-qt/build/bin/klingnet-qt bin/klingnet-qt

# Dev mode for GUI wallet (hot reload)
dev-qt:
	cd cmd/klingnet-qt && $(HOME)/go/bin/wails dev -tags webkit2_41

# Run all tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Run all checks
check: fmt vet test

# Clean build artifacts
clean:
	rm -rf bin/ dist/
	rm -f coverage.out coverage.html

# Install dependencies
deps:
	go mod download
	go mod tidy

# ── Cross-compilation ──────────────────────────────────────────────────
# klingnetd + klingnet-cli: statically linked, no CGO needed.
# klingnet-qt: requires CGO + platform SDK (webkit2gtk on Linux, WebView2 on
# Windows, WKWebView on macOS). Must be built natively on each platform or
# with appropriate cross-compilers (see build-qt-* targets below).

DIST = dist
BINS = klingnetd klingnet-cli
CMDS = ./cmd/klingnetd ./cmd/klingnet-cli
WAILS_CROSS_IMAGE ?= klingnet-qt-windows-cross:latest
WAILS_CROSS_DOCKERFILE ?= cmd/klingnet-qt/docker/windows-cross.Dockerfile

define build-platform
	@mkdir -p $(DIST)/$(1)-$(2)
	@echo "Building $(1)/$(2)..."
	@CGO_ENABLED=0 GOOS=$(1) GOARCH=$(2) go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(1)-$(2)/klingnetd$(3) ./cmd/klingnetd
	@CGO_ENABLED=0 GOOS=$(1) GOARCH=$(2) go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(1)-$(2)/klingnet-cli$(3) ./cmd/klingnet-cli
endef

# Individual platform targets
build-linux-amd64:
	$(call build-platform,linux,amd64,)

build-linux-arm64:
	$(call build-platform,linux,arm64,)

build-darwin-amd64:
	$(call build-platform,darwin,amd64,)

build-darwin-arm64:
	$(call build-platform,darwin,arm64,)

build-windows-amd64:
	$(call build-platform,windows,amd64,.exe)

# Build all cross-compilation targets (CLI tools only)
release: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64
	@echo ""
	@echo "Release builds:"
	@ls -lh $(DIST)/*/klingnetd* $(DIST)/*/klingnet-cli*
	@echo ""
	@echo "Done. Binaries in $(DIST)/"

# ── GUI cross-compilation (requires native build environment) ──────────
# Wails uses CGO + platform webview. These targets must be run on the
# target platform (or in a container with the right SDK).
#
# Linux:   apt install libwebkit2gtk-4.1-dev  (or webkit2gtk4.1-devel on Fedora)
# macOS:   Xcode command line tools (WKWebView is bundled)
# Windows: WebView2 runtime (bundled by Wails at build time)
#
# Usage: run on the target machine or in CI with the right OS image.

build-qt-linux:
	cd cmd/klingnet-qt && $(HOME)/go/bin/wails build -tags webkit2_41
	@mkdir -p $(DIST)/linux-$(shell go env GOARCH)
	cp cmd/klingnet-qt/build/bin/klingnet-qt $(DIST)/linux-$(shell go env GOARCH)/klingnet-qt

build-qt-darwin:
	@if [ "$$(uname -s)" != "Darwin" ]; then \
		echo "build-qt-darwin must run on macOS host/runner."; \
		exit 1; \
	fi
	cd cmd/klingnet-qt && $(HOME)/go/bin/wails build
	@mkdir -p $(DIST)/darwin-$(shell go env GOARCH)
	cp -R cmd/klingnet-qt/build/bin/klingnet-qt.app $(DIST)/darwin-$(shell go env GOARCH)/klingnet-qt.app

# macOS universal app bundle (arm64 + amd64) - run on macOS host/runner.
build-qt-macos-universal:
	@if [ "$$(uname -s)" != "Darwin" ]; then \
		echo "build-qt-macos-universal must run on macOS host/runner."; \
		exit 1; \
	fi
	cd cmd/klingnet-qt && $(HOME)/go/bin/wails build -platform darwin/universal
	@mkdir -p $(DIST)/darwin-universal
	cp -R cmd/klingnet-qt/build/bin/klingnet-qt.app $(DIST)/darwin-universal/klingnet-qt.app

# Native Windows build target (run on Windows host with Wails installed)
build-qt-windows-native:
	cd cmd/klingnet-qt && $(HOME)/go/bin/wails build -platform windows/amd64
	@mkdir -p $(DIST)
	@mkdir -p $(DIST)/windows-amd64
	cp cmd/klingnet-qt/build/bin/klingnet-qt.exe $(DIST)/klingnet-qt.exe
	cp cmd/klingnet-qt/build/bin/klingnet-qt.exe $(DIST)/windows-amd64/klingnet-qt.exe

# Docker image with mingw-w64 + Wails for Linux -> Windows cross compile
build-qt-windows-image:
	docker build -f $(WAILS_CROSS_DOCKERFILE) -t $(WAILS_CROSS_IMAGE) .

# Linux-hosted Windows cross build via Docker. Output: dist/klingnet-qt.exe
build-qt-windows-docker: build-qt-windows-image
	@mkdir -p $(DIST)
	@mkdir -p $(DIST)/windows-amd64
	docker run --rm \
		-u $$(id -u):$$(id -g) \
		-e HOME=/tmp \
		-e GOCACHE=/tmp/gocache \
		-e GOMODCACHE=/tmp/gomodcache \
		-v "$(PWD)":/workspace \
		-w /workspace \
		$(WAILS_CROSS_IMAGE) \
		bash -lc 'export PATH=/usr/local/go/bin:/go/bin:$$PATH; cd cmd/klingnet-qt && wails build -clean -platform windows/amd64 && cp build/bin/klingnet-qt.exe /workspace/$(DIST)/klingnet-qt.exe && cp build/bin/klingnet-qt.exe /workspace/$(DIST)/windows-amd64/klingnet-qt.exe'

# Auto-select Windows GUI build path based on host OS
build-qt-windows:
ifeq ($(shell uname -s),Linux)
	$(MAKE) build-qt-windows-docker
else
	$(MAKE) build-qt-windows-native
endif

# Full release: CLI tools (cross-compiled) + GUI (native platform only)
release-full: release build-qt-linux
