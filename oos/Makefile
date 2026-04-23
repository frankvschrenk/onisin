# =============================================================================
# oos/Makefile — lokaler Entwicklungs-Build
# Für Release-Builds: make compile aus /onisin/ Root
# =============================================================================

VERSION := $(shell date +"%y.%-j.%H%M")
LDFLAGS  = -ldflags="-X 'main.VERSION=$(VERSION)'"

.PHONY: build clean

build:
	@echo "⚙️  oos lokal — $(VERSION)"
	@go build $(LDFLAGS) -o oos .
	@echo "✅ ./oos"

clean:
	@rm -f oos
