# =============================================================================
# Makefile — onisin root (run from /onisin/)
#
# make compile         — build all modules into dist/
# make compile-oos     — build oos (Fyne desktop client)
# make compile-ooso    — build ooso (importer + designer)
# make compile-oosp    — build oosp (plugin server)
# make compile-oosb    — build oosb (MCP bridge, retired — kept for reference)
# make compile-oos-demo— build oos-demo (native process manager)
# make package-oos-app — wrap oos_macos in OOS.app bundle (macOS only)
# make package-ooso-app— wrap ooso_macos in Ooso.app bundle (macOS only)
# make tidy            — go mod tidy across all modules
# make release         — create GitHub release from dist/
# make deploy          — build + push Docker images
# make clean           — remove dist/
# =============================================================================

REGISTRY      = docker.io/oosai
IMAGE_OOSP    = $(REGISTRY)/oosp
PLATFORM      = linux/amd64,linux/arm64

DIST          = $(shell pwd)/dist
VERSION_FILE  = $(DIST)/.version
RELEASES_DIR  = $(shell pwd)/releases

COMPILE_VERSION := $(shell date +"%y.%-j.%H%M")
NATIVE_OS       := $(shell go env GOOS)
NATIVE_ARCH     := $(shell go env GOARCH)

LDFLAGS = -ldflags="-X 'main.VERSION=$(COMPILE_VERSION)'"

export CGO_LDFLAGS = -Wl,-no_warn_duplicate_libraries

.PHONY: compile compile-oos compile-ooso compile-oosb compile-oosp \
        compile-oos-demo package-oos-app package-ooso-app release deploy \
        clean tidy help run-oosp-local check-oos-dsl check-oos-common

# -----------------------------------------------------------------------------
# compile — all modules
# -----------------------------------------------------------------------------

compile: compile-oos compile-ooso compile-oosb compile-oosp compile-oos-demo
	@echo $(COMPILE_VERSION) > $(VERSION_FILE)
ifeq ($(NATIVE_OS),darwin)
	@$(MAKE) --no-print-directory package-oos-app
	@$(MAKE) --no-print-directory package-ooso-app
endif
	@echo ""
	@echo "✅ dist/"
	@ls -lh $(DIST)/

# -----------------------------------------------------------------------------
# Shared module checks
# -----------------------------------------------------------------------------

check-oos-dsl:
	@cd oos-dsl && go vet ./...

check-oos-common:
	@cd oos-common && go vet ./...

# -----------------------------------------------------------------------------
# oos — Fyne desktop client (CGO, native)
# -----------------------------------------------------------------------------

compile-oos:
	@mkdir -p $(DIST)
	@echo "⚙️  oos — $(COMPILE_VERSION)"
ifeq ($(NATIVE_OS),darwin)
	@cd oos && \
		CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oos_mac_amd64 . && \
		CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/oos_mac_arm64 .
	@lipo -create -output $(DIST)/oos_macos $(DIST)/oos_mac_amd64 $(DIST)/oos_mac_arm64
	@rm $(DIST)/oos_mac_amd64 $(DIST)/oos_mac_arm64
	@echo "✅ dist/oos_macos"
else ifeq ($(NATIVE_OS),linux)
	@cd oos && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oos_linux_amd64 .
	@echo "✅ dist/oos_linux_amd64"
endif

# -----------------------------------------------------------------------------
# package-oos-app — wrap dist/oos_macos in an OOS.app bundle (macOS only)
#
# The universal binary produced by compile-oos has a filename that macOS uses
# as the menu-bar title (currently "oos_macos"). Wrapping it in a proper .app
# bundle with CFBundleName=OOS fixes that and gives us a clickable app icon.
#
# Strategy:
#   1. Ensure dist/oos_macos exists (build it if not).
#   2. Let `fyne package` generate the bundle skeleton (Info.plist, icon,
#      directory layout) using --executable to avoid a redundant rebuild.
#   3. Move the result into dist/OOS.app.
# -----------------------------------------------------------------------------

package-oos-app:
ifneq ($(NATIVE_OS),darwin)
	@echo "⚠️  package-oos-app is macOS only (current: $(NATIVE_OS))"
	@exit 1
endif
	@if [ ! -f $(DIST)/oos_macos ]; then \
		echo "⚙️  dist/oos_macos missing — building first"; \
		$(MAKE) compile-oos; \
	fi
	@echo "📦 packaging OOS.app — $(COMPILE_VERSION)"
	@rm -rf $(DIST)/OOS.app $(DIST)/oos/OOS.app
	@cd oos && fyne package \
		--os darwin \
		--name OOS \
		--app-id com.onisin.oos \
		--app-version $(COMPILE_VERSION) \
		--icon assets/icon.icns \
		--executable $(DIST)/oos_macos \
		--release
	@mv oos/OOS.app $(DIST)/OOS.app
	@mv $(DIST)/OOS.app/Contents/MacOS/oos_macos $(DIST)/OOS.app/Contents/MacOS/OOS
	@/usr/libexec/PlistBuddy -c "Set :CFBundleExecutable OOS" $(DIST)/OOS.app/Contents/Info.plist
	@echo "✅ dist/OOS.app"

# -----------------------------------------------------------------------------
# package-ooso-app — wrap dist/ooso_macos in an Ooso.app bundle (macOS only)
#
# Same pattern as package-oos-app: reuse the universal binary produced by
# compile-ooso, let fyne package build the bundle skeleton, then rename
# the internal binary and patch CFBundleExecutable so `ps` and the Dock
# both show "Ooso" instead of the platform-suffixed file name.
#
# Icon and bundle-id are deliberately parallel to the oos bundle — they
# read as one product family rather than two unrelated apps.
# -----------------------------------------------------------------------------

package-ooso-app:
ifneq ($(NATIVE_OS),darwin)
	@echo "⚠️  package-ooso-app is macOS only (current: $(NATIVE_OS))"
	@exit 1
endif
	@if [ ! -f $(DIST)/ooso_macos ]; then \
		echo "⚙️  dist/ooso_macos missing — building first"; \
		$(MAKE) compile-ooso; \
	fi
	@echo "📦 packaging Ooso.app — $(COMPILE_VERSION)"
	@rm -rf $(DIST)/Ooso.app $(DIST)/ooso/Ooso.app
	@cd ooso && fyne package \
		--os darwin \
		--name Ooso \
		--app-id com.onisin.ooso \
		--app-version $(COMPILE_VERSION) \
		--icon assets/icon.icns \
		--executable $(DIST)/ooso_macos \
		--release
	@mv ooso/Ooso.app $(DIST)/Ooso.app
	@mv $(DIST)/Ooso.app/Contents/MacOS/ooso_macos $(DIST)/Ooso.app/Contents/MacOS/Ooso
	@/usr/libexec/PlistBuddy -c "Set :CFBundleExecutable Ooso" $(DIST)/Ooso.app/Contents/Info.plist
	@echo "✅ dist/Ooso.app"

# -----------------------------------------------------------------------------
# ooso — importer + designer (CGO, native)
# -----------------------------------------------------------------------------

compile-ooso: check-oos-dsl
	@mkdir -p $(DIST)
	@echo "⚙️  ooso — $(COMPILE_VERSION)"
ifeq ($(NATIVE_OS),darwin)
	@cd ooso && \
		CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/ooso_mac_amd64 . && \
		CGO_ENABLED=1 GOOS=darwin GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/ooso_mac_arm64 .
	@lipo -create -output $(DIST)/ooso_macos $(DIST)/ooso_mac_amd64 $(DIST)/ooso_mac_arm64
	@rm $(DIST)/ooso_mac_amd64 $(DIST)/ooso_mac_arm64
	@echo "✅ dist/ooso_macos"
else ifeq ($(NATIVE_OS),linux)
	@cd ooso && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/ooso_linux_amd64 .
	@echo "✅ dist/ooso_linux_amd64"
endif

# -----------------------------------------------------------------------------
# oosb — MCP bridge (CGO_ENABLED=0, all platforms)
# -----------------------------------------------------------------------------

compile-oosb:
	@mkdir -p $(DIST)
	@echo "⚙️  oosb — $(COMPILE_VERSION)"
	@cd oosb && \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oosb_mac_amd64 . && \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/oosb_mac_arm64 .
	@lipo -create -output $(DIST)/oosb_macos $(DIST)/oosb_mac_amd64 $(DIST)/oosb_mac_arm64
	@rm $(DIST)/oosb_mac_amd64 $(DIST)/oosb_mac_arm64
	@cd oosb && \
		CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oosb_linux_amd64 . && \
		CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oosb_windows_amd64.exe .
	@echo "✅ dist/oosb_*"

# -----------------------------------------------------------------------------
# oosp — plugin server (CGO_ENABLED=0, all platforms)
# -----------------------------------------------------------------------------

compile-oosp:
	@mkdir -p $(DIST)
	@echo "⚙️  oosp — $(COMPILE_VERSION)"
	@cd oosp && \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oosp_mac_amd64 . && \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/oosp_mac_arm64 .
	@lipo -create -output $(DIST)/oosp_macos $(DIST)/oosp_mac_amd64 $(DIST)/oosp_mac_arm64
	@rm $(DIST)/oosp_mac_amd64 $(DIST)/oosp_mac_arm64
	@cd oosp && \
		CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oosp_linux_amd64 . && \
		CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/oosp_linux_arm64 . && \
		CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oosp_windows_amd64.exe .
	@echo "✅ dist/oosp_*"

# -----------------------------------------------------------------------------
# oos-demo — native process manager
# -----------------------------------------------------------------------------

compile-oos-demo:
	@mkdir -p $(DIST)
	@echo "⚙️  oos-demo — $(COMPILE_VERSION)"
	@cd oos-demo && \
		CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oos-demo_mac_amd64 . && \
		CGO_ENABLED=0 GOOS=darwin GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/oos-demo_mac_arm64 .
	@lipo -create -output $(DIST)/oos-demo_macos $(DIST)/oos-demo_mac_amd64 $(DIST)/oos-demo_mac_arm64
	@rm $(DIST)/oos-demo_mac_amd64 $(DIST)/oos-demo_mac_arm64
	@cd oos-demo && \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/oos-demo_linux_amd64 . && \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/oos-demo_linux_arm64 .
	@echo "✅ dist/oos-demo_*"

# -----------------------------------------------------------------------------
# go mod tidy
# -----------------------------------------------------------------------------

tidy:
	@cd oos-dsl    && go mod tidy
	@cd oos-common && go mod tidy
	@cd ooso       && go mod tidy
	@cd oosp       && go mod tidy
	@cd oos        && go mod tidy
	@cd oos-demo   && go mod tidy
	@echo "✅ go mod tidy — all modules"

# -----------------------------------------------------------------------------
# GitHub release
# -----------------------------------------------------------------------------

release:
	$(eval VERSION := $(shell cat $(VERSION_FILE)))
	@echo "📦 GitHub release $(VERSION)..."
	@echo $(VERSION) > $(RELEASES_DIR)/version.txt
	@cd $(RELEASES_DIR) && \
		git add version.txt && \
		git diff --cached --quiet || git commit -m "release $(VERSION)" && \
		git push
	@gh release create $(VERSION) \
		$(DIST)/oos_macos \
		$(DIST)/oos_linux_amd64 \
		$(DIST)/ooso_macos \
		$(DIST)/ooso_linux_amd64 \
		$(DIST)/oosp_linux_amd64 \
		$(DIST)/oos-demo_macos \
		$(DIST)/oos-demo_linux_amd64 \
		$(DIST)/oos-demo_linux_arm64 \
		--repo onisin.com/releases \
		--title "OOS $(VERSION)" \
		--notes "OOS $(VERSION)"
	@echo "✅ https://github.com/onisin.com/releases/releases/tag/$(VERSION)"

# -----------------------------------------------------------------------------
# Docker deploy
# -----------------------------------------------------------------------------

deploy: deploy-oosp
	@echo "✅ all Docker images deployed"

deploy-oosp:
	$(eval VERSION := $(shell cat $(VERSION_FILE)))
	@docker buildx build --no-cache \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		-f demo/oosp.Dockerfile \
		-t $(IMAGE_OOSP):$(VERSION) \
		-t $(IMAGE_OOSP):latest \
		--push $(DIST)
	@echo "✅ $(IMAGE_OOSP):$(VERSION)"

# -----------------------------------------------------------------------------
# Run oosp locally
# -----------------------------------------------------------------------------

run-oosp-local:
	@OOSP_SERVER_ADDR=":9100" \
	 OOSP_VAULT_URL="http://localhost:8200" \
	 OOSP_VAULT_TOKEN="oos-dev-root-token" \
	 OOSP_DEBUG="true" \
	 $(DIST)/oosp_macos --unsecure

# -----------------------------------------------------------------------------
# Cleanup
# -----------------------------------------------------------------------------

clean:
	@rm -rf dist

help:
	@echo ""
	@echo "  make compile           — build all modules into dist/"
	@echo "  make compile-oos       — build oos (Fyne desktop client)"
	@echo "  make compile-ooso      — build ooso (importer + designer)"
	@echo "  make compile-oosp      — build oosp (plugin server)"
	@echo "  make compile-oosb      — build oosb (MCP bridge)"
	@echo "  make compile-oos-demo  — build oos-demo (native process manager)"
	@echo "  make package-oos-app   — wrap oos_macos in OOS.app bundle (macOS)"
	@echo "  make package-ooso-app  — wrap ooso_macos in Ooso.app bundle (macOS)"
	@echo "  make tidy              — go mod tidy across all modules"
	@echo "  make release           — create GitHub release"
	@echo "  make deploy            — build + push Docker images"
	@echo "  make clean             — remove dist/"
	@echo ""
