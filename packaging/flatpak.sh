#!/bin/bash
# Build the Flatpak locally and install it (or export a bundle). The build
# itself runs offline inside flatpak-builder's sandbox: Go modules are
# vendored into a clean export of the checkout here on the host first, then
# the build uses -mod=vendor with GOPROXY=off and no network share.
#
# Why Flatpak: the GNOME runtime brings its own GTK 4 / libadwaita /
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
# Klient's GTK bindings need GLib >= 2.89.3 and WebKitGTK 6.0. The stable
# GNOME 51 runtime provides GLib 2.90.1, WebKitGTK 2.54 and libadwaita 1.10,
# which is the first stable runtime new enough to build Klient.
RUNTIME_VER=51
GO=$(command -v go || echo /bin/go)

for tool in flatpak flatpak-builder "$GO"; do
	command -v "$tool" >/dev/null 2>&1 || { echo "missing tool: $tool" >&2; exit 1; }
done

# Ensure a user-level flathub remote (the system one is not visible to
# --user installs). Idempotent. The Go toolchain is bundled by the manifest,
# so no SDK extension is needed.
flatpak remote-add --user --if-not-exists flathub \
	https://dl.flathub.org/repo/flathub.flatpakrepo

# Runtime and SDK from flathub.
flatpak install -y --user flathub \
	org.gnome.Platform//$RUNTIME_VER \
	org.gnome.Sdk//$RUNTIME_VER

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

# Build offline (no --share=network) once, exporting into a local repo, then
# install from that repo and (optionally) bundle it. Building to the repo in a
# single pass avoids recompiling the (slow) CGo GTK bindings twice.
flatpak-builder --user --force-clean --disable-rofiles-fuse \
	--repo="$OUT/repo" \
	--state-dir="$OUT/.flatpak-builder" \
	"$OUT/build-dir" "$MANIFEST"

# Install (or update) from the just-built repo. default-branch is "stable".
flatpak remote-add --user --if-not-exists --no-gpg-verify klient-local "$OUT/repo"
flatpak install -y --user --reinstall klient-local "$APP_ID" || \
	flatpak update -y --user "$APP_ID"

if [ "$BUNDLE" = "--bundle" ]; then
	# The last argument is the APP branch (default-branch in the manifest),
	# not the runtime version.
	flatpak build-bundle "$OUT/repo" "$OUT/klient.flatpak" "$APP_ID" stable
	echo "bundle: $OUT/klient.flatpak"
fi

echo "installed: flatpak run $APP_ID"
