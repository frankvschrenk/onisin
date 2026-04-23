#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
# OOS Build Script – nativ auf der jeweiligen Plattform ausführen
#
# USAGE:
#   ./build.sh mac    → macOS Universal Binary (nur auf Mac)
#   ./build.sh linux  → Linux amd64 (nur auf Linux)
#   ./build.sh win    → Windows amd64 (nur auf Windows)
# ─────────────────────────────────────────────────────────────

set -e

# Neuesten Tag direkt von GitHub holen (kein lokaler fetch nötig)
VERSION=$(git ls-remote --tags --sort=-version:refname git@github.com:onisin.com/releases.git 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "dev")
OUT="../dist"
LDFLAGS="-s -w"

mkdir -p "${OUT}"

echo "═══════════════════════════════════════"
echo "  OOS Build  ${VERSION}"
echo "═══════════════════════════════════════"

case "${1}" in
  mac)
    echo "▶ macOS (arm64 + amd64 → Universal)"
    CGO_ENABLED=1 \
    CGO_LDFLAGS="-framework UniformTypeIdentifiers" \
    CGO_CFLAGS="-Wno-deprecated-declarations" \
    GOARCH=arm64 go build -tags desktop,production \
      -ldflags "${LDFLAGS} -X main.VERSION=${VERSION}" -o "${OUT}/oos-darwin-arm64" .

    CGO_ENABLED=1 \
    CGO_LDFLAGS="-framework UniformTypeIdentifiers" \
    CGO_CFLAGS="-Wno-deprecated-declarations" \
    GOARCH=amd64 go build -tags desktop,production \
      -ldflags "${LDFLAGS} -X main.VERSION=${VERSION}" -o "${OUT}/oos-darwin-amd64" .

    lipo -create -output "${OUT}/oos_macos" \
      "${OUT}/oos-darwin-arm64" "${OUT}/oos-darwin-amd64"
    rm "${OUT}/oos-darwin-arm64" "${OUT}/oos-darwin-amd64"
    echo "  ✅ ${OUT}/oos_macos"
    ;;

  linux)
    echo "▶ Linux (amd64)"
    CGO_ENABLED=1 GOARCH=amd64 \
      go build -tags desktop,production \
      -ldflags "${LDFLAGS} -X main.VERSION=${VERSION}" -o "${OUT}/oos_linux_amd64" .
    echo "  ✅ ${OUT}/oos_linux_amd64"
    ;;

  win)
    echo "▶ Windows (amd64)"
    CGO_ENABLED=1 GOARCH=amd64 \
      go build -tags desktop,production \
      -ldflags "${LDFLAGS} -H windowsgui -X main.VERSION=${VERSION}" -o "${OUT}/oos_windows_amd64.exe" .
    echo "  ✅ ${OUT}/oos_windows_amd64.exe"
    ;;

  *)
    echo "Verwendung: ./build.sh [mac|linux|win]"
    exit 1
    ;;
esac

# version.json für Release generieren
cat > "${OUT}/version.json" <<EOF
{
  "version": "${VERSION}",
  "url_mac":   "https://github.com/onisin.com/releases/releases/download/${VERSION}/oos_macos",
  "url_linux": "https://github.com/onisin.com/releases/releases/download/${VERSION}/oos_linux_amd64",
  "url_win":   "https://github.com/onisin.com/releases/releases/download/${VERSION}/oos_windows_amd64.exe"
}
EOF
echo "✅ version.json → ${OUT}/version.json"

echo ""
ls -lh "${OUT}/"
