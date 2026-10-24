#!/bin/bash
# Build libklient.a and Klient.app (Apple Silicon). No Homebrew required.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MACOS="$ROOT/macos"
BUILD="$MACOS/build"
APP="$BUILD/Klient.app"
VERSION="${VERSION:-0.11.7}"
export PATH="/usr/local/go/bin:${PATH:-}"

mkdir -p "$BUILD"
echo "==> Go c-archive"
(
  cd "$ROOT"
  go build -buildmode=c-archive \
    -ldflags "-s -w -X github.com/Imbecile6197/klient/internal/macbridge.Version=${VERSION}" \
    -o "$BUILD/libklient.a" ./cmd/klientcore
)

echo "==> Assemble Klient.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$MACOS/Klient/Info.plist" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $VERSION" "$APP/Contents/Info.plist"

# Resources: localizations, AppIcon.icns, etc.
if [[ -d "$MACOS/Klient/Resources" ]]; then
  # Copy lproj bundles and loose resources; skip intermediate iconset/source PNG
  rsync -a --exclude 'AppIcon.iconset' --exclude 'KlientAppIcon-1024.png' \
    --exclude '.DS_Store' \
    "$MACOS/Klient/Resources/" "$APP/Contents/Resources/"
fi
if [[ ! -f "$APP/Contents/Resources/AppIcon.icns" ]]; then
  echo "warning: AppIcon.icns missing under Resources (Dock will show generic icon)" >&2
fi

SDK="$(xcrun --sdk macosx --show-sdk-path)"
echo "==> ObjC helpers (SDK $SDK)"
clang -c -fobjc-arc \
  -isysroot "$SDK" \
  -target arm64-apple-macos14.0 \
  -o "$BUILD/ExceptionCatcher.o" \
  "$MACOS/Klient/ExceptionCatcher.m"

echo "==> Swift (SDK $SDK)"
# shellcheck disable=SC2046
swiftc -parse-as-library \
  -sdk "$SDK" \
  -target arm64-apple-macos14.0 \
  -O \
  -import-objc-header "$MACOS/Klient/KlientBridge.h" \
  -I "$BUILD" \
  "$MACOS/Klient/Sources/"*.swift \
  "$BUILD/libklient.a" \
  "$BUILD/ExceptionCatcher.o" \
  -lresolv \
  -framework Foundation \
  -framework AppKit \
  -framework SwiftUI \
  -framework WebKit \
  -framework UserNotifications \
  -framework CoreServices \
  -framework Security \
  -framework CoreFoundation \
  -framework Contacts \
  -framework ServiceManagement \
  -o "$APP/Contents/MacOS/Klient"

chmod +x "$APP/Contents/MacOS/Klient"

echo "==> Ad-hoc codesign (Developer ID can replace this later)"
codesign --force --deep --sign - "$APP"

echo "Built $APP"
echo "Open with: open \"$APP\""
echo "Demo:     open --env KLIENT_DEMO=1 \"$APP\""
