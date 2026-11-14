#!/bin/bash
# Publish the Flatpak built by packaging/flatpak.sh:
#   - signs it into a fresh OSTree repository for GitHub Pages
#     (https://imbecile6197.github.io/klient/repo) with the key in
#     packaging/flatpak/repo-key.fpr,
#   - writes klient.flatpakrepo and eu.libormacak.Klient.flatpakref there,
#   - exports build/release/klient.flatpak, a bundle that registers that
#     repository on installation, so updates then come through flatpak
#     (GNOME Software or `flatpak update`).
# The repository holds only the newest build: flatpak downloads just the
# changed files, so no history is needed.
#
# --push then publishes the site built before (without rebuilding it, so the
# bundle stays the one in SHA256SUMS): it replaces the gh-pages branch with a
# single commit, so old builds do not pile up in git.
#
# Usage: packaging/flatpak-repo.sh VERSION          # build the site and bundle
#        packaging/flatpak-repo.sh VERSION --push   # publish the built site
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:?usage: packaging/flatpak-repo.sh VERSION [--push]}
PUSH=${2:-}
APP_ID=eu.libormacak.Klient
BASE=https://imbecile6197.github.io/klient
KEY=$(cat packaging/flatpak/repo-key.fpr)
PUBKEY=packaging/flatpak/klient-repo.gpg
SRC=build/flatpak/repo
SITE=build/flatpak/site

push() {
	[ -f "$SITE/repo/summary" ] || { echo "no site in $SITE – run without --push first" >&2; exit 1; }
	tmp=$(mktemp -d)
	cp -a "$SITE"/. "$tmp"/
	git -C "$tmp" init -q -b gh-pages
	git -C "$tmp" add -A
	git -C "$tmp" -c user.name="$(git config user.name)" -c user.email="$(git config user.email)" \
		commit -q -m "Flatpak repository for Klient $VERSION"
	git -C "$tmp" -c credential.helper= -c credential.helper='!gh auth git-credential' \
		push -q --force "$(git remote get-url origin)" gh-pages
	rm -rf "$tmp"
	# The first publication also switches GitHub Pages on for gh-pages.
	repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)
	gh api "repos/$repo/pages" >/dev/null 2>&1 ||
		gh api -X POST "repos/$repo/pages" -f 'source[branch]=gh-pages' -f 'source[path]=/' >/dev/null
	echo "published: $BASE/"
}
if [ "$PUSH" = "--push" ]; then
	push
	exit 0
fi

[ -d "$SRC" ] || { echo "no $SRC – run packaging/flatpak.sh $VERSION first" >&2; exit 1; }
gpg --list-secret-keys "$KEY" >/dev/null 2>&1 || { echo "the signing key $KEY is not in this GPG keyring" >&2; exit 1; }
# The build must be this version (the version is baked into the binary).
REF=app/$APP_ID/x86_64/stable
flatpak repo --branches "$SRC" | grep -q "$REF" || { echo "$SRC has no $REF" >&2; exit 1; }

rm -rf "$SITE" && mkdir -p "$SITE"
cp -a "$SRC" "$SITE/repo"
for ref in $(flatpak repo --branches "$SRC" | awk '{print $1}' | grep -E '^(app|runtime)/'); do
	flatpak build-sign --gpg-sign="$KEY" "$SITE/repo" "$(cut -d/ -f2 <<<"$ref")" "$(cut -d/ -f4 <<<"$ref")" \
		$( [ "${ref%%/*}" = runtime ] && echo --runtime )
done
flatpak build-update-repo --gpg-sign="$KEY" --title="Klient" --comment="Klient mail client" \
	--homepage=https://github.com/Imbecile6197/klient --default-branch=stable \
	--prune "$SITE/repo"

GPGKEY=$(base64 -w0 "$PUBKEY")
cat > "$SITE/klient.flatpakrepo" <<EOR
[Flatpak Repo]
Title=Klient
Url=$BASE/repo/
Homepage=https://github.com/Imbecile6197/klient
Comment=Klient mail client
GPGKey=$GPGKEY
EOR
cat > "$SITE/$APP_ID.flatpakref" <<EOR
[Flatpak Ref]
Name=$APP_ID
Branch=stable
Title=Klient
Url=$BASE/repo/
SuggestRemoteName=klient
IsRuntime=false
RuntimeRepo=https://dl.flathub.org/repo/flathub.flatpakrepo
GPGKey=$GPGKEY
EOR
cp "$PUBKEY" "$SITE/klient-repo.gpg"
touch "$SITE/.nojekyll"
cat > "$SITE/index.html" <<'EOR'
<!doctype html>
<meta charset="utf-8">
<title>Klient Flatpak repository</title>
<p>This is the Flatpak repository of <a href="https://github.com/Imbecile6197/klient">Klient</a>.
Install with <code>flatpak install --user https://imbecile6197.github.io/klient/eu.libormacak.Klient.flatpakref</code>
or download <code>klient.flatpak</code> from the <a href="https://github.com/Imbecile6197/klient/releases/latest">latest release</a>.</p>
EOR

# The bundle: installing it adds the repository above as the "klient"
# remote (with its key) and Flathub for the GNOME runtime.
mkdir -p build/release
flatpak build-bundle --repo-url="$BASE/repo/" \
	--runtime-repo=https://dl.flathub.org/repo/flathub.flatpakrepo \
	--gpg-keys="$PUBKEY" \
	"$SITE/repo" build/release/klient.flatpak "$APP_ID" stable
echo "bundle: build/release/klient.flatpak"

