#!/bin/sh
# Cuts a release: builds the app, tags, pushes, uploads the zip to GitHub, and
# updates the cask and formula with the new version and checksums.
#
#   ./publish.sh 0.1.0
#
# Needs the GitHub CLI:  brew install gh && gh auth login
# If ../homebrew-tap exists, the updated cask and formula are copied and pushed
# there too.
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

# The formula builds from the tag's source tarball, which only exists once the
# tag is pushed, so its checksum is filled in afterwards.
TARBALL="https://github.com/$REPO/archive/refs/tags/v$VERSION.tar.gz"
SRC_SHA=$(curl -fsSL "$TARBALL" | shasum -a 256 | cut -d' ' -f1)
sed -i '' -e "s|^  url \".*\"|  url \"$TARBALL\"|" \
          -e "s|^  sha256 \".*\"|  sha256 \"$SRC_SHA\"|" Formula/displayctl.rb
git add Formula/displayctl.rb
git diff --cached --quiet || { git commit -q -m "formula: v$VERSION"; git push -q origin main; }

if [ -d ../homebrew-tap ]; then
	mkdir -p ../homebrew-tap/Casks ../homebrew-tap/Formula
	cp Casks/displayctl.rb ../homebrew-tap/Casks/displayctl.rb
	cp Formula/displayctl.rb ../homebrew-tap/Formula/displayctl.rb
	git -C ../homebrew-tap add Casks/displayctl.rb Formula/displayctl.rb
	git -C ../homebrew-tap diff --cached --quiet || {
		git -C ../homebrew-tap commit -q -m "displayctl $VERSION"
		# HEAD:main rather than a bare push, which fails on a fresh tap with no upstream.
		git -C ../homebrew-tap push -q origin HEAD:main
		echo "tap updated"
	}
else
	echo "copy Casks/displayctl.rb and Formula/displayctl.rb into your homebrew-tap repo"
fi

echo
echo "released v$VERSION  sha256=$SHA"
echo "install with: brew tap josesujith/tap && brew install --cask displayctl"
