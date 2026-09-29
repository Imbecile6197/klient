#!/bin/bash
# Publish a Klient release on GitHub: builds the RPM and uploads it with a
# SHA256SUMS file. Klient's updater installs only packages whose checksum
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
RPM=$(ls build/rpm/RPMS/x86_64/klient-"$VERSION"-*.x86_64.rpm)
(cd "$(dirname "$RPM")" && sha256sum "$(basename "$RPM")" > SHA256SUMS)

# Release notes: the newest %changelog entry of the spec.
NOTES=$(awk '/^%changelog/{c=1; next} c && /^\*/{if (n++) exit; next} c' packaging/klient.spec | sed '/^$/d')

git -c credential.helper= -c credential.helper='!gh auth git-credential' push origin main "$TAG"
gh release create "$TAG" "$RPM" "$(dirname "$RPM")/SHA256SUMS" \
	--title "Klient $VERSION" \
	--notes "$NOTES

Instalace: \`sudo dnf install ./$(basename "$RPM")\` – nainstalovaný Klient si další verze stáhne sám (Předvolby → Zprávy → Aktualizace)."
