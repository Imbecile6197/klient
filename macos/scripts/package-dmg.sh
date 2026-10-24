#!/bin/bash
# Create Klient-VERSION-macos-arm64.dmg from the built .app.
# Does not upload or notarize. Sign with Developer ID before distributing.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/macos/build"
APP="$BUILD/Klient.app"
VERSION="${VERSION:-0.11.11}"
DMG="$BUILD/Klient-${VERSION}-macos-arm64.dmg"
STAGE="$BUILD/dmg-stage"

if [[ ! -d "$APP" ]]; then
  echo "missing $APP – run macos/scripts/build.sh first" >&2
  exit 1
fi

rm -rf "$STAGE" "$DMG"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/Klient.app"
ln -s /Applications "$STAGE/Applications"

hdiutil create -volname "Klient" -srcfolder "$STAGE" -ov -format UDZO "$DMG"
rm -rf "$STAGE"

(
  cd "$BUILD"
  shasum -a 256 "$(basename "$DMG")" | tee SHA256SUMS-macos.txt
)

echo "DMG: $DMG"
echo "After Developer ID sign + notarize, upload with:"
echo "  gh release upload v${VERSION} \"$DMG\" \"$BUILD/SHA256SUMS-macos.txt\""
echo "(only after the author says yes for that version)"
