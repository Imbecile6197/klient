#!/bin/bash
# Build the Flatpak locally and install it (or export a bundle). The build
# itself runs offline inside flatpak-builder's sandbox: Go modules are
# vendored into a clean export of the checkout here on the host first, then
# the build uses -mod=vendor with GOPROXY=off and no network share.
#
# Why Flatpak: the GNOME 49 runtime brings its own GTK 4 / libadwaita /
# WebKitGTK 6.0 and GLib, so Klient runs even on hosts whose system GLib is
# too old for the native package (e.g. Manjaro with GLib 2.88).
#
# Usage:
#   packaging/flatpak.sh 0.10.4            # build and install (--user)
#   packaging/flatpak.sh 0.10.4 --bundle   # also export build/flatpak/klient.flatpak
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:?usage: packaging/flatpak.sh VERSION [--bundle]}
BUNDLE=${2:-}
APP_ID=eu.libormacak.Klient
RUNTIME_VER=49
GO=$(command -v go || echo /bin/go)

for tool in flatpak flatpak-builder "$GO"; do
	command -v "$tool" >/dev/null 2>&1 || { echo "missing tool: $tool" >&2; exit 1; }
done

# Runtime, SDK and the Go SDK extension (no-op if already installed).
flatpak install -y --user flathub \
	org.gnome.Platform//$RUNTIME_VER \
	org.gnome.Sdk//$RUNTIME_VER \
	org.freedesktop.Sdk.Extension.golang//25.08

OUT=build/flatpak
SRC=$OUT/src
rm -rf "$SRC"
mkdir -p "$SRC" "$OUT/.flatpak-builder"

# Clean export of the checkout (tracked files only), then vendor the Go
# modules into it so the sandboxed build needs no network.
git archive --format=tar HEAD | tar -x -C "$SRC"
( cd "$SRC" && GOFLAGS=-mod=mod GOPROXY= "$GO" mod vendor )

# A local manifest that points the dir source at the vendored export and
# bakes in the version (flatpak-builder does not forward host env vars).
MANIFEST=$OUT/$APP_ID.local.yml
sed -e "s#path: \.\./\.\.#path: src#" \
    -e "s#\${KLIENT_VERSION:-dev}#$VERSION#" \
    packaging/flatpak/$APP_ID.yml > "$MANIFEST"

# Build offline (no --share=network). --install puts it in the user install.
flatpak-builder --user --force-clean --disable-rofiles-fuse \
	--install \
	--state-dir="$OUT/.flatpak-builder" \
	"$OUT/build-dir" "$MANIFEST"

if [ "$BUNDLE" = "--bundle" ]; then
	flatpak-builder --user --force-clean --disable-rofiles-fuse \
		--repo="$OUT/repo" \
		--state-dir="$OUT/.flatpak-builder" \
		"$OUT/build-dir" "$MANIFEST"
	flatpak build-bundle "$OUT/repo" "$OUT/klient.flatpak" "$APP_ID" "$RUNTIME_VER"
	echo "bundle: $OUT/klient.flatpak"
fi

echo "installed: flatpak run $APP_ID"
