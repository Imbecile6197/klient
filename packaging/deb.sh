#!/bin/bash
# Build the Debian/Ubuntu package in a container (podman): the binary must be
# linked against Ubuntu's libraries, not Fedora's. The Go toolchain and the
# module cache of the host are mounted read-only, so nothing is downloaded
# except the container image.
#
# Usage: packaging/deb.sh 0.7.3   → build/deb/klient_0.7.3_amd64.deb
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:?použití: packaging/deb.sh VERZE}
APP_ID=eu.libormacak.Klient
IMAGE=klient-deb-build
GOROOT=$(go env GOROOT)
GOMODCACHE=$(go env GOMODCACHE)

go mod download
podman build -q -t "$IMAGE" -f packaging/deb/Containerfile packaging/deb >/dev/null
mkdir -p build/deb-gocache
rm -rf build/deb
# SELinux labels are disabled for the run so the host's Go can execute.
podman run --rm --security-opt label=disable \
	-v "$PWD":/src -v "$GOROOT":/goroot:ro -v "$GOMODCACHE":/gomod:ro \
	-v "$PWD/build/deb-gocache":/gocache \
	-e GOROOT=/goroot -e GOMODCACHE=/gomod -e GOCACHE=/gocache \
	-e GOFLAGS=-mod=readonly -e GOPROXY=off -e GOTOOLCHAIN=local \
	-e VERSION="$VERSION" -e APP_ID="$APP_ID" \
	-w /src "$IMAGE" bash -euo pipefail -c '
		/goroot/bin/go build -ldflags "-s -w -X github.com/Imbecile6197/klient/internal/ui.Version=$VERSION" -o build/deb/klient ./cmd/klient
		R=build/deb/root
		install -Dm755 build/deb/klient $R/usr/bin/klient
		install -Dm644 data/$APP_ID.desktop $R/usr/share/applications/$APP_ID.desktop
		install -Dm644 data/$APP_ID.svg $R/usr/share/icons/hicolor/scalable/apps/$APP_ID.svg
		install -Dm644 data/$APP_ID-symbolic.svg $R/usr/share/icons/hicolor/symbolic/apps/$APP_ID-symbolic.svg
		install -Dm644 data/$APP_ID.metainfo.xml $R/usr/share/metainfo/$APP_ID.metainfo.xml
		install -Dm644 README.md $R/usr/share/doc/klient/README.md
		install -Dm644 LICENSE $R/usr/share/doc/klient/copyright
		desktop-file-validate $R/usr/share/applications/$APP_ID.desktop
		appstreamcli validate --no-net --pedantic $R/usr/share/metainfo/$APP_ID.metainfo.xml >/dev/null || \
			appstreamcli validate --no-net $R/usr/share/metainfo/$APP_ID.metainfo.xml

		# Shared-library dependencies from the binary itself.
		mkdir -p build/deb/debian && echo "Source: klient" > build/deb/debian/control
		DEPS=$(cd build/deb && dpkg-shlibdeps -O root/usr/bin/klient 2>/dev/null | sed -n "s/^shlibs:Depends=//p")

		mkdir -p $R/DEBIAN
		cat > $R/DEBIAN/control <<CONTROL
Package: klient
Version: $VERSION
Architecture: amd64
Maintainer: Imbecile6197 <Imbecile6197@users.noreply.github.com>
Installed-Size: $(du -sk $R/usr | cut -f1)
Depends: $DEPS, gnome-keyring | kwalletmanager | keepassxc, libglib2.0-bin, pkexec, hicolor-icon-theme
Recommends: gnome-shell-extension-appindicator, libenchant-2-2, hunspell-cs, hunspell-en-us
Section: mail
Priority: optional
Homepage: https://github.com/Imbecile6197/klient
Description: Mail client with PGP, an AI assistant and an AI spam filter
 A GNOME mail client for Proton Mail (directly over its API, without Proton
 Bridge), Gmail, Seznam.cz and any IMAP/SMTP mailbox: end-to-end encryption
 and PGP/MIME, conversations, safe HTML mail, a rich-text editor, an AI
 assistant and an AI-driven spam filter with block lists (cloud providers or
 a local model), an encrypted offline cache, rules, undo send and scheduled
 sending. In English and Czech.
CONTROL
		dpkg-deb --root-owner-group --build $R build/deb/klient_${VERSION}_amd64.deb
		rm -rf $R build/deb/debian build/deb/klient
	'
ls -1 build/deb/*.deb
