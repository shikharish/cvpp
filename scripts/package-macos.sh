#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "macOS packages must be built on macOS so their bundle can be signed and verified." >&2
  exit 1
fi

VERSION="${1:-dev}"
BUNDLE_VERSION="${VERSION#v}"
if [[ ! "$BUNDLE_VERSION" =~ ^[0-9]+([.][0-9]+){0,2}$ ]]; then
  BUNDLE_VERSION="0.0.0"
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
BUILD_DIR="$DIST/macos-build"
APP_DIR="$BUILD_DIR/CV++.app"

rm -rf "$BUILD_DIR"
mkdir -p "$APP_DIR/Contents/MacOS"

cd "$ROOT"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X cvpp/internal/editorserver.version=$VERSION" \
  -o "$APP_DIR/Contents/MacOS/CV++" \
  ./cmd/cvpp

cat > "$APP_DIR/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>CV++</string>
  <key>CFBundleIdentifier</key>
  <string>org.cvpp.app</string>
  <key>CFBundleName</key>
  <string>CV++</string>
  <key>CFBundleDisplayName</key>
  <string>CV++</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$BUNDLE_VERSION</string>
  <key>CFBundleVersion</key>
  <string>$BUNDLE_VERSION</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST

plutil -lint "$APP_DIR/Contents/Info.plist"
xattr -cr "$APP_DIR"
codesign --force --deep --options runtime --timestamp=none --sign - "$APP_DIR"
codesign --verify --deep --strict --verbose=2 "$APP_DIR"

rm -f "$DIST/CV++-macos-arm64.zip"
ditto -c -k --keepParent "$APP_DIR" "$DIST/CV++-macos-arm64.zip"
echo "Wrote $DIST/CV++-macos-arm64.zip"
