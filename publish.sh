#!/bin/sh
# Cuts a release: builds the app, tags, pushes, uploads the zip to GitHub, and
# updates the cask with the new version and checksum.
#
#   ./publish.sh 0.1.0
#
# Needs the GitHub CLI:  brew install gh && gh auth login
# If ../homebrew-tap exists, the updated cask is copied and pushed there too.
set -e

cd "$(dirname "$0")"
VERSION="${1:-0.1.0}"
REPO="josesujith/displayctl"
ZIP="dist/DisplayCtl-$VERSION.zip"

command -v gh >/dev/null || { echo "install the GitHub CLI first: brew install gh" >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "sign in first: gh auth login" >&2; exit 1; }
git remote get-url origin >/dev/null 2>&1 || {
	echo "no git remote. create the repo, then:" >&2
	echo "  git remote add origin git@github.com:$REPO.git" >&2
	exit 1
}

./make-app.sh >/dev/null
mkdir -p dist
rm -f "$ZIP"
ditto -c -k --sequesterRsrc --keepParent DisplayCtl.app "$ZIP"
SHA=$(shasum -a 256 "$ZIP" | cut -d' ' -f1)

# Update the cask before tagging, so the tag holds the matching checksum.
sed -i '' -e "s|^  version \".*\"|  version \"$VERSION\"|" \
          -e "s|^  sha256 \".*\"|  sha256 \"$SHA\"|" Casks/displayctl.rb
git add Casks/displayctl.rb
git diff --cached --quiet || git commit -q -m "cask: v$VERSION"

git push -q origin main
git tag -f "v$VERSION"
git push -qf origin "v$VERSION"

gh release create "v$VERSION" "$ZIP" --repo "$REPO" --title "v$VERSION" --generate-notes \
	|| gh release upload "v$VERSION" "$ZIP" --repo "$REPO" --clobber

if [ -d ../homebrew-tap ]; then
	mkdir -p ../homebrew-tap/Casks
	cp Casks/displayctl.rb ../homebrew-tap/Casks/displayctl.rb
	git -C ../homebrew-tap add Casks/displayctl.rb
	git -C ../homebrew-tap diff --cached --quiet || {
		git -C ../homebrew-tap commit -q -m "displayctl $VERSION"
		git -C ../homebrew-tap push -q
		echo "tap updated"
	}
else
	echo "copy Casks/displayctl.rb into your homebrew-tap repo"
fi

echo
echo "released v$VERSION  sha256=$SHA"
echo "install with: brew tap josesujith/tap && brew install --cask displayctl"
