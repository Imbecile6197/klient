#!/bin/bash
# Publish a Klient release on GitHub: builds the RPM (Fedora) and the DEB
# (Ubuntu, in a container) and uploads them with a SHA256SUMS file. Klient's updater installs only packages whose checksum
# matches both SHA256SUMS and GitHub's own asset digest.
#
# Usage: packaging/release.sh 0.6.8   (the tree must be committed and tagged v0.6.8)
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:?použití: packaging/release.sh VERZE}
TAG=v$VERSION

if [ -n "$(git status --porcelain)" ]; then
	echo "V pracovní složce jsou neuložené změny – nejdřív je commitněte." >&2
	exit 1
fi
if [ "$(git rev-parse "$TAG^{commit}" 2>/dev/null)" != "$(git rev-parse HEAD)" ]; then
	echo "Značka $TAG neukazuje na aktuální commit." >&2
	exit 1
fi

make rpm VERSION="$VERSION"
packaging/deb.sh "$VERSION"
OUT=build/release
rm -rf "$OUT" && mkdir -p "$OUT"
cp build/rpm/RPMS/x86_64/klient-"$VERSION"-*.x86_64.rpm build/deb/klient_"$VERSION"_amd64.deb "$OUT"/
# Copies with a fixed name give stable links for the first installation:
# https://github.com/Imbecile6197/klient/releases/latest/download/klient.x86_64.rpm
# https://github.com/Imbecile6197/klient/releases/latest/download/klient_amd64.deb
cp "$OUT"/klient-"$VERSION"-*.x86_64.rpm "$OUT"/klient.x86_64.rpm
cp "$OUT"/klient_"$VERSION"_amd64.deb "$OUT"/klient_amd64.deb
(cd "$OUT" && sha256sum klient* > SHA256SUMS)

# Release notes: packaging/release-notes/VERSION.md (English and Czech) if it
# exists, otherwise the newest %changelog entry of the spec.
if [ -f "packaging/release-notes/$VERSION.md" ]; then
	NOTES=$(cat "packaging/release-notes/$VERSION.md")
else
	NOTES=$(awk '/^%changelog/{c=1; next} c && /^\*/{if (n++) exit; next} c' packaging/klient.spec | sed '/^$/d')
	NOTES="$NOTES

Install on Fedora: \`sudo dnf install https://github.com/Imbecile6197/klient/releases/latest/download/klient.x86_64.rpm\`, on Ubuntu 26.04+: download \`klient_amd64.deb\` and run \`sudo apt install ./klient_amd64.deb\` – an installed Klient then updates itself (Preferences → Messages → Updates)."
fi

git -c credential.helper= -c credential.helper='!gh auth git-credential' push origin main "$TAG"
gh release create "$TAG" "$OUT"/* \
	--title "Klient $VERSION" \
	--notes "$NOTES"
