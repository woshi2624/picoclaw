.PHONY: all build install uninstall clean help test

# Build variables
BINARY_NAME=picoclaw
BUILD_DIR=build
CMD_DIR=cmd/$(BINARY_NAME)
MAIN_GO=$(CMD_DIR)/main.go

# Version
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell $(GO) version | awk '{print $$3}')
CONFIG_PKG=github.com/sipeed/picoclaw/pkg/config
LDFLAGS=-ldflags "-X $(CONFIG_PKG).Version=$(VERSION) -X $(CONFIG_PKG).GitCommit=$(GIT_COMMIT) -X $(CONFIG_PKG).BuildTime=$(BUILD_TIME) -X $(CONFIG_PKG).GoVersion=$(GO_VERSION) -s -w"

# Go variables
GO?=CGO_ENABLED=0 go
GOFLAGS?=-v -tags stdjson

# macOS .app bundle settings
DARWIN_APP_NAME=PicoClaw
DARWIN_BUNDLE_ID=com.sipeed.picoclaw
DARWIN_ICON_SRC=$(CURDIR)/assets/clawdchat-icon.png

# Create macOS .app bundle; $(1) = platform-arch suffix (e.g. darwin-arm64)
define CREATE_DARWIN_APP
	@echo "  Creating $(DARWIN_APP_NAME).app for $(1)..."
	@rm -rf "$(BUILD_DIR)/$(DARWIN_APP_NAME).app"
	@mkdir -p "$(BUILD_DIR)/$(DARWIN_APP_NAME).app/Contents/MacOS"
	@mkdir -p "$(BUILD_DIR)/$(DARWIN_APP_NAME).app/Contents/Resources"
	@cp "$(BUILD_DIR)/$(BINARY_NAME)-$(1)" "$(BUILD_DIR)/$(DARWIN_APP_NAME).app/Contents/MacOS/$(BINARY_NAME)"
	@chmod +x "$(BUILD_DIR)/$(DARWIN_APP_NAME).app/Contents/MacOS/$(BINARY_NAME)"
	@printf '<?xml version="1.0" encoding="UTF-8"?>\n\
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n\
<plist version="1.0">\n<dict>\n\
\t<key>CFBundleName</key><string>$(DARWIN_APP_NAME)</string>\n\
\t<key>CFBundleDisplayName</key><string>$(DARWIN_APP_NAME)</string>\n\
\t<key>CFBundleIdentifier</key><string>$(DARWIN_BUNDLE_ID)</string>\n\
\t<key>CFBundleVersion</key><string>$(VERSION)</string>\n\
\t<key>CFBundleShortVersionString</key><string>$(VERSION)</string>\n\
\t<key>CFBundlePackageType</key><string>APPL</string>\n\
\t<key>CFBundleExecutable</key><string>$(BINARY_NAME)</string>\n\
\t<key>CFBundleIconFile</key><string>AppIcon</string>\n\
\t<key>LSMinimumSystemVersion</key><string>10.13</string>\n\
\t<key>NSHighResolutionCapable</key><true/>\n\
</dict>\n</plist>\n' > "$(BUILD_DIR)/$(DARWIN_APP_NAME).app/Contents/Info.plist"
	@if command -v sips >/dev/null 2>&1 && command -v iconutil >/dev/null 2>&1; then \
		mkdir -p "$(BUILD_DIR)/AppIcon.iconset" && \
		sips -z 16 16     "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_16x16.png"      >/dev/null 2>&1 && \
		sips -z 32 32     "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_16x16@2x.png"   >/dev/null 2>&1 && \
		sips -z 32 32     "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_32x32.png"      >/dev/null 2>&1 && \
		sips -z 64 64     "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_32x32@2x.png"   >/dev/null 2>&1 && \
		sips -z 128 128   "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_128x128.png"    >/dev/null 2>&1 && \
		sips -z 256 256   "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_128x128@2x.png" >/dev/null 2>&1 && \
		sips -z 256 256   "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_256x256.png"    >/dev/null 2>&1 && \
		sips -z 512 512   "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_256x256@2x.png" >/dev/null 2>&1 && \
		sips -z 512 512   "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_512x512.png"    >/dev/null 2>&1 && \
		sips -z 1024 1024 "$(DARWIN_ICON_SRC)" --out "$(BUILD_DIR)/AppIcon.iconset/icon_512x512@2x.png" >/dev/null 2>&1 && \
		iconutil -c icns "$(BUILD_DIR)/AppIcon.iconset" --output "$(BUILD_DIR)/$(DARWIN_APP_NAME).app/Contents/Resources/AppIcon.icns" && \
		rm -rf "$(BUILD_DIR)/AppIcon.iconset" && \
		echo "  Icon embedded"; \
	else \
		echo "  Warning: sips/iconutil not available, skipping icon (macOS only)"; \
	fi
	@if command -v codesign >/dev/null 2>&1; then \
		codesign --force --deep --sign - "$(BUILD_DIR)/$(DARWIN_APP_NAME).app" && \
		echo "  Ad-hoc signed"; \
	else \
		echo "  Warning: codesign not available, app may be blocked by Gatekeeper"; \
	fi
endef

# Patch MIPS LE ELF e_flags (offset 36) for NaN2008-only kernels (e.g. Ingenic X2600).
#
# Bytes (octal): \004 \024 \000 \160  →  little-endian 0x70001404
#   0x70000000  EF_MIPS_ARCH_32R2   MIPS32 Release 2
#   0x00001000  EF_MIPS_ABI_O32     O32 ABI
#   0x00000400  EF_MIPS_NAN2008     IEEE 754-2008 NaN encoding
#   0x00000004  EF_MIPS_CPIC        PIC calling sequence
#
# Go's GOMIPS=softfloat emits no FP instructions, so the NaN mode is irrelevant
# at runtime — this is purely an ELF metadata fix to satisfy the kernel's check.
# patchelf cannot modify e_flags; dd at a fixed offset is the most portable way.
#
# Ref: https://codebrowser.dev/linux/linux/arch/mips/include/asm/elf.h.html
define PATCH_MIPS_FLAGS
	@if [ -f "$(1)" ]; then \
		printf '\004\024\000\160' | dd of=$(1) bs=1 seek=36 count=4 conv=notrunc 2>/dev/null || \
		{ echo "Error: failed to patch MIPS e_flags for $(1)"; exit 1; }; \
	else \
		echo "Error: $(1) not found, cannot patch MIPS e_flags"; exit 1; \
	fi
endef

# Golangci-lint
GOLANGCI_LINT?=golangci-lint

# Installation
INSTALL_PREFIX?=$(HOME)/.local
INSTALL_BIN_DIR=$(INSTALL_PREFIX)/bin
INSTALL_MAN_DIR=$(INSTALL_PREFIX)/share/man/man1
INSTALL_TMP_SUFFIX=.new

# Workspace and Skills
PICOCLAW_HOME?=$(HOME)/.picoclaw
WORKSPACE_DIR?=$(PICOCLAW_HOME)/workspace
WORKSPACE_SKILLS_DIR=$(WORKSPACE_DIR)/skills
BUILTIN_SKILLS_DIR=$(CURDIR)/skills

# OS detection
UNAME_S:=$(shell uname -s)
UNAME_M:=$(shell uname -m)

# Platform-specific settings
ifeq ($(UNAME_S),Linux)
	PLATFORM=linux
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),aarch64)
		ARCH=arm64
	else ifeq ($(UNAME_M),armv81)
		ARCH=arm64
	else ifeq ($(UNAME_M),loongarch64)
		ARCH=loong64
	else ifeq ($(UNAME_M),riscv64)
		ARCH=riscv64
	else ifeq ($(UNAME_M),mipsel)
		ARCH=mipsle
	else
		ARCH=$(UNAME_M)
	endif
else ifeq ($(UNAME_S),Darwin)
	PLATFORM=darwin
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),arm64)
		ARCH=arm64
	else
		ARCH=$(UNAME_M)
	endif
else
	PLATFORM=$(UNAME_S)
	ARCH=$(UNAME_M)
endif

BINARY_PATH=$(BUILD_DIR)/$(BINARY_NAME)-$(PLATFORM)-$(ARCH)

# Default target
all: build

## generate: Run generate
generate:
	@echo "Run generate..."
	@rm -r ./$(CMD_DIR)/workspace 2>/dev/null || true
	@$(GO) generate ./...
	@echo "Run generate complete"

## build: Build the picoclaw binary for current platform
build: generate
	@echo "Building $(BINARY_NAME) for $(PLATFORM)/$(ARCH)..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_PATH) ./$(CMD_DIR)
	@echo "Build complete: $(BINARY_PATH)"
	@ln -sf $(BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(BINARY_NAME)

## build-darwin-app: Build and package picoclaw as a macOS .app bundle (darwin only)
build-darwin-app: generate
	@echo "Building $(BINARY_NAME) for darwin/arm64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	$(call CREATE_DARWIN_APP,darwin-arm64)
	@echo "App bundle: $(BUILD_DIR)/$(DARWIN_APP_NAME).app"

## build-launcher: Build the picoclaw-launcher (web console) binary
build-launcher:
	@echo "Building picoclaw-launcher for $(PLATFORM)/$(ARCH)..."
	@mkdir -p $(BUILD_DIR)
	@if [ ! -f web/backend/dist/index.html ]; then \
		echo "Building frontend..."; \
		cd web/frontend && pnpm install && pnpm build:backend; \
	fi
	@$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/picoclaw-launcher-$(PLATFORM)-$(ARCH) ./web/backend
	@ln -sf picoclaw-launcher-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/picoclaw-launcher
	@echo "Build complete: $(BUILD_DIR)/picoclaw-launcher"

## build-whatsapp-native: Build with WhatsApp native (whatsmeow) support; larger binary
build-whatsapp-native: generate
## @echo "Building $(BINARY_NAME) with WhatsApp native for $(PLATFORM)/$(ARCH)..."
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	GOOS=linux GOARCH=loong64 $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-loong64 ./$(CMD_DIR)
	GOOS=linux GOARCH=riscv64 $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle ./$(CMD_DIR)
	$(call PATCH_MIPS_FLAGS,$(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle)
	GOOS=darwin GOARCH=arm64 $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build -tags whatsapp_native $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
## @$(GO) build $(GOFLAGS) -tags whatsapp_native $(LDFLAGS) -o $(BINARY_PATH) ./$(CMD_DIR)
	@echo "Build complete"
##	@ln -sf $(BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(BINARY_NAME)

## build-linux-arm: Build for Linux ARMv7 (e.g. Raspberry Pi Zero 2 W 32-bit)
build-linux-arm: generate
	@echo "Building for linux/arm (GOARM=7)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./$(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-arm"

## build-linux-arm64: Build for Linux ARM64 (e.g. Raspberry Pi Zero 2 W 64-bit)
build-linux-arm64: generate
	@echo "Building for linux/arm64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64"

## build-linux-mipsle: Build for Linux MIPS32 LE
build-linux-mipsle: generate
	@echo "Building for linux/mipsle (softfloat)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle ./$(CMD_DIR)
	$(call PATCH_MIPS_FLAGS,$(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle"

## build-pi-zero: Build for Raspberry Pi Zero 2 W (32-bit and 64-bit)
build-pi-zero: build-linux-arm build-linux-arm64
	@echo "Pi Zero 2 W builds: $(BUILD_DIR)/$(BINARY_NAME)-linux-arm (32-bit), $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 (64-bit)"

## build-all: Build picoclaw for all platforms and package into zip archives
build-all: generate
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	GOOS=linux GOARCH=loong64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-loong64 ./$(CMD_DIR)
	GOOS=linux GOARCH=riscv64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle ./$(CMD_DIR)
	$(call PATCH_MIPS_FLAGS,$(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-armv7 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@echo "All builds complete. Packaging zip archives..."
	@for platform in linux-amd64 linux-arm linux-arm64 linux-armv7 linux-loong64 linux-riscv64 linux-mipsle; do \
		mkdir -p "$(BUILD_DIR)/.stage/$(BINARY_NAME)-$$platform" && \
		cp "$(BUILD_DIR)/$(BINARY_NAME)-$$platform" "$(BUILD_DIR)/.stage/$(BINARY_NAME)-$$platform/$(BINARY_NAME)" && \
		(cd "$(BUILD_DIR)/.stage" && zip -r "../$(BINARY_NAME)-$$platform.zip" "$(BINARY_NAME)-$$platform/") && \
		rm -rf "$(BUILD_DIR)/.stage/$(BINARY_NAME)-$$platform" && \
		echo "  Packaged: $(BUILD_DIR)/$(BINARY_NAME)-$$platform.zip"; \
	done
	$(call CREATE_DARWIN_APP,darwin-arm64)
	@mkdir -p "$(BUILD_DIR)/.stage"
	@cp -r "$(BUILD_DIR)/$(DARWIN_APP_NAME).app" "$(BUILD_DIR)/.stage/$(DARWIN_APP_NAME).app"
	@(cd "$(BUILD_DIR)/.stage" && zip -r "../$(BINARY_NAME)-darwin-arm64.zip" "$(DARWIN_APP_NAME).app/")
	@rm -rf "$(BUILD_DIR)/.stage/$(DARWIN_APP_NAME).app"
	@echo "  Packaged: $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64.zip"
	@mkdir -p "$(BUILD_DIR)/.stage/$(BINARY_NAME)-windows-amd64" && \
	cp "$(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe" "$(BUILD_DIR)/.stage/$(BINARY_NAME)-windows-amd64/$(BINARY_NAME).exe" && \
	(cd "$(BUILD_DIR)/.stage" && zip -r "../$(BINARY_NAME)-windows-amd64.zip" "$(BINARY_NAME)-windows-amd64/") && \
	rm -rf "$(BUILD_DIR)/.stage/$(BINARY_NAME)-windows-amd64" && \
	echo "  Packaged: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.zip"
	@rm -rf "$(BUILD_DIR)/.stage"
	@echo "All zip archives created in $(BUILD_DIR)/"

## install: Install picoclaw to system and copy builtin skills
install: build
	@echo "Installing $(BINARY_NAME)..."
	@mkdir -p $(INSTALL_BIN_DIR)
	# Copy binary with temporary suffix to ensure atomic update
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX)
	@chmod +x $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX)
	@mv -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX) $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Installed binary to $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@echo "Installation complete!"

## uninstall: Remove picoclaw from system
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	@rm -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Removed binary from $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@echo "Note: Only the executable file has been deleted."
	@echo "If you need to delete all configurations (config.json, workspace, etc.), run 'make uninstall-all'"

## uninstall-all: Remove picoclaw and all data
uninstall-all:
	@echo "Removing workspace and skills..."
	@rm -rf $(PICOCLAW_HOME)
	@echo "Removed workspace: $(PICOCLAW_HOME)"
	@echo "Complete uninstallation done!"

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

## vet: Run go vet for static analysis
vet: generate
	@$(GO) vet ./...

## test: Test Go code
test: generate
	@$(GO) test ./...

## fmt: Format Go code
fmt:
	@$(GOLANGCI_LINT) fmt

## lint: Run linters
lint:
	@$(GOLANGCI_LINT) run

## fix: Fix linting issues
fix:
	@$(GOLANGCI_LINT) run --fix

## deps: Download dependencies
deps:
	@$(GO) mod download
	@$(GO) mod verify

## update-deps: Update dependencies
update-deps:
	@$(GO) get -u ./...
	@$(GO) mod tidy

## check: Run vet, fmt, and verify dependencies
check: deps fmt vet test

## run: Build and run picoclaw
run: build
	@$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

## docker-build: Build Docker image (minimal Alpine-based)
docker-build:
	@echo "Building minimal Docker image (Alpine-based)..."
	docker compose -f docker/docker-compose.yml build picoclaw-agent picoclaw-gateway

## docker-build-full: Build Docker image with full MCP support (Node.js 24)
docker-build-full:
	@echo "Building full-featured Docker image (Node.js 24)..."
	docker compose -f docker/docker-compose.full.yml build picoclaw-agent picoclaw-gateway

## docker-test: Test MCP tools in Docker container
docker-test:
	@echo "Testing MCP tools in Docker..."
	@chmod +x scripts/test-docker-mcp.sh
	@./scripts/test-docker-mcp.sh

## docker-run: Run picoclaw gateway in Docker (Alpine-based)
docker-run:
	docker compose -f docker/docker-compose.yml --profile gateway up

## docker-run-full: Run picoclaw gateway in Docker (full-featured)
docker-run-full:
	docker compose -f docker/docker-compose.full.yml --profile gateway up

## docker-run-agent: Run picoclaw agent in Docker (interactive, Alpine-based)
docker-run-agent:
	docker compose -f docker/docker-compose.yml run --rm picoclaw-agent

## docker-run-agent-full: Run picoclaw agent in Docker (interactive, full-featured)
docker-run-agent-full:
	docker compose -f docker/docker-compose.full.yml run --rm picoclaw-agent

## docker-clean: Clean Docker images and volumes
docker-clean:
	docker compose -f docker/docker-compose.yml down -v
	docker compose -f docker/docker-compose.full.yml down -v
	docker rmi picoclaw:latest picoclaw:full 2>/dev/null || true

## help: Show this help message
help:
	@echo "picoclaw Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sort | awk -F': ' '{printf "  %-16s %s\n", substr($$1, 4), $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make build              # Build for current platform"
	@echo "  make install            # Install to ~/.local/bin"
	@echo "  make uninstall          # Remove from /usr/local/bin"
	@echo "  make install-skills     # Install skills to workspace"
	@echo "  make docker-build       # Build minimal Docker image"
	@echo "  make docker-test        # Test MCP tools in Docker"
	@echo ""
	@echo "Environment Variables:"
	@echo "  INSTALL_PREFIX          # Installation prefix (default: ~/.local)"
	@echo "  WORKSPACE_DIR           # Workspace directory (default: ~/.picoclaw/workspace)"
	@echo "  VERSION                 # Version string (default: git describe)"
	@echo ""
	@echo "Current Configuration:"
	@echo "  Platform: $(PLATFORM)/$(ARCH)"
	@echo "  Binary: $(BINARY_PATH)"
	@echo "  Install Prefix: $(INSTALL_PREFIX)"
	@echo "  Workspace: $(WORKSPACE_DIR)"
