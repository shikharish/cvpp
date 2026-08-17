#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
rm -rf "$DIST"
mkdir -p "$DIST/build"
cd "$ROOT"

build() {
  local goos="$1" goarch="$2" output="$3"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X cvpp/internal/editorserver.version=$VERSION" -o "$output" ./cmd/cvpp
}

mkdir -p "$DIST/build/windows"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui -X cvpp/internal/editorserver.version=$VERSION" -o "$DIST/build/windows/CV++.exe" ./cmd/cvpp
build windows amd64 "$DIST/build/windows/cvpp.exe"
(cd "$DIST/build/windows" && zip -q -r "$DIST/CV++-windows-amd64.zip" .)

for arch in amd64 arm64; do
  build darwin "$arch" "$DIST/build/cvpp-darwin-$arch"
  APP_DIR="$DIST/build/cvpp-$arch.app"
  mkdir -p "$APP_DIR/Contents/MacOS"
  cp "$DIST/build/cvpp-darwin-$arch" "$APP_DIR/Contents/MacOS/CV++"
  cat > "$APP_DIR/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>CV++</string><key>CFBundleIdentifier</key><string>org.cvpp.app</string><key>CFBundleName</key><string>CV++</string><key>CFBundleVersion</key><string>$VERSION</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
  mv "$APP_DIR" "$DIST/build/CV++.app"
  (cd "$DIST/build" && zip -q -r "$DIST/CV++-darwin-$arch.zip" "CV++.app")
  rm -rf "$DIST/build/CV++.app"
done

build linux amd64 "$DIST/build/cvpp-linux-amd64"
cp "$DIST/build/cvpp-linux-amd64" "$DIST/build/CV++"
tar -C "$DIST/build" -czf "$DIST/CV++-linux-amd64.tar.gz" cvpp-linux-amd64
# The AppImage launcher is deliberately tiny and self-contained; it runs the
# same portable binary and avoids a runtime dependency on the repository.
mkdir -p "$DIST/build/AppDir/usr/bin"
cp "$DIST/build/cvpp-linux-amd64" "$DIST/build/AppDir/usr/bin/cvpp"
cat > "$DIST/build/AppDir/cvpp.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=CV++
Exec=cvpp
Icon=cvpp
Categories=Utility;
DESKTOP
cp "$ROOT/assets/cvpp.svg" "$DIST/build/AppDir/cvpp.svg"
cat > "$DIST/build/AppDir/AppRun" <<'APP'
#!/bin/sh
HERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec "$HERE/usr/bin/cvpp" "$@"
APP
chmod +x "$DIST/build/AppDir/AppRun"
if command -v appimagetool >/dev/null 2>&1; then
  ARCH=x86_64 appimagetool "$DIST/build/AppDir" "$DIST/CV++-linux-amd64.AppImage"
else
  # Local dry-runs can still inspect the complete AppDir without downloading
  # a third-party tool. CI installs appimagetool and emits a real AppImage.
  tar -C "$DIST/build/AppDir" -czf "$DIST/CV++-linux-amd64.AppImage" .
fi

(cd "$DIST" && sha256sum *.zip *.tar.gz *.AppImage > SHA256SUMS)
echo "Wrote release assets to $DIST"
