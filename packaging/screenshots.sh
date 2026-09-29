#!/bin/bash
# Regenerates the screenshots in docs/screenshots/<language>/ with the demo
# account (made-up people and mail; the user's accounts are not touched).
# A Klient window flashes on the screen for about half a minute per language.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:-$(git describe --tags --abbrev=0 | sed 's/^v//')}
BIN=$(mktemp -d)/klient
go build -ldflags "-X github.com/Imbecile6197/klient/internal/ui.Version=$VERSION" -o "$BIN" ./cmd/klient

for lang in en cs; do
	out=docs/screenshots/$lang
	rm -rf "$out"
	KLIENT_DEMO=1 KLIENT_LANGUAGE=$lang KLIENT_SCREENSHOTS="$out" timeout 120 "$BIN"
	ls "$out"
done
