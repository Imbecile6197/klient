#!/bin/bash
# Sign and notarize Klient.app with a Developer ID certificate.
# Required env:
#   SIGN_IDENTITY  e.g. "Developer ID Application: … (TEAMID)"
#   APPLE_ID       Apple ID email for notarytool
#   TEAM_ID        10-char Team ID
#   NOTARY_PASSWORD  app-specific password (or use keychain profile)
# Optional: NOTARY_PROFILE=AC_PASSWORD to use stored credentials.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
APP="${1:-$ROOT/macos/build/Klient.app}"
IDENTITY="${SIGN_IDENTITY:?set SIGN_IDENTITY}"

if [[ ! -d "$APP" ]]; then
  echo "missing $APP" >&2
  exit 1
fi

echo "==> codesign"
codesign --force --deep --options runtime --timestamp \
  --sign "$IDENTITY" "$APP"
codesign --verify --verbose=2 "$APP"

echo "==> notarize"
ZIP="${APP%.app}-notarize.zip"
rm -f "$ZIP"
ditto -c -k --keepParent "$APP" "$ZIP"

if [[ -n "${NOTARY_PROFILE:-}" ]]; then
  xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait
else
  xcrun notarytool submit "$ZIP" \
    --apple-id "${APPLE_ID:?}" \
    --team-id "${TEAM_ID:?}" \
    --password "${NOTARY_PASSWORD:?}" \
    --wait
fi

xcrun stapler staple "$APP"
rm -f "$ZIP"
echo "Signed and notarized: $APP"
