#!/bin/bash
# Build the Arch/Manjaro package in a container (podman): the binary must be
# linked against Arch's libraries, not the host's. The Go toolchain and the
# module cache of the host are mounted read-only, so nothing is downloaded
# except the container image (and the pacman packages while it is built).
#
# Usage: packaging/arch.sh 0.7.3   → build/arch/klient-0.7.3-1-x86_64.pkg.tar.zst
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:?usage: packaging/arch.sh VERSION}
IMAGE=klient-arch-build

# go.mod may require a newer toolchain than the system Go; run from the module
# dir so `go` resolves (and, if needed, downloads) the right one. GOROOT then
# points at that exact toolchain, which is mounted into the container so the
# build is both offline and the correct version. GOTOOLCHAIN is left at its
# default here on purpose, so a missing toolchain is fetched once.
GO=$(command -v go || echo /bin/go)
"$GO" mod download
GOROOT=$("$GO" env GOROOT)
GOMODCACHE=$("$GO" env GOMODCACHE)
podman build -q -t "$IMAGE" -f packaging/arch/Containerfile packaging/arch >/dev/null
mkdir -p build/arch
rm -f build/arch/*.pkg.tar.zst
# SELinux labels are disabled for the run so the host's Go can execute.
podman run --rm --security-opt label=disable \
	-v "$PWD":/src:ro -v "$GOROOT":/goroot:ro -v "$GOMODCACHE":/gomod:ro \
	-v "$PWD/build/arch":/out \
	-e VERSION="$VERSION" \
	"$IMAGE" bash -euo pipefail -c '
		# Lay out a writable work tree owned by the unprivileged build user.
		# makepkg (--noextract) reads sources from ./src/, so the project tree
		# goes to ./src/klient to match $srcdir/klient in the PKGBUILD.
		W=/home/build/pkg
		install -d -o build -g build "$W/src" "$W/gocache"
		cp -a /src/. "$W/src/klient/"
		rm -rf "$W/src/klient/build"
		# Stamp the version into the PKGBUILD (makepkg reads pkgver from the
		# file, not the environment).
		sed "s/^pkgver=.*/pkgver=$VERSION/" /src/packaging/arch/PKGBUILD > "$W/PKGBUILD"
		chown -R build:build "$W"

		# GOCACHE must be writable by the build user, so keep it in the work
		# tree. makepkg writes the package into the work tree (owned by build);
		# /out is a host bind mount the unprivileged user cannot write to, so
		# copy out afterwards.
		runuser -u build -- env \
			GOROOT=/goroot GOMODCACHE=/gomod GOCACHE="$W/gocache" \
			GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local \
			PATH=/goroot/bin:/usr/local/sbin:/usr/local/bin:/usr/bin \
			PKGDEST="$W" \
			bash -c "cd $W && makepkg --noextract --nodeps --noconfirm --skipinteg"

		cp "$W"/*.pkg.tar.zst /out/
		chown "$(stat -c %u:%g /src)" /out/*.pkg.tar.zst
	'
ls -1 build/arch/*.pkg.tar.zst
