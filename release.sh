#!/bin/sh
# Builds the release tarball to attach to a GitHub release, and prints the
# checksum to paste into the formula.
#
#   ./release.sh 0.1.0
set -e

cd "$(dirname "$0")"
VERSION="${1:-0.1.0}"
TAR="dist/displayctl-$VERSION-macos-arm64.tar.gz"

rm -rf dist
mkdir -p dist/displayctl
go build -trimpath -ldflags "-s -w" -o dist/displayctl/displayctl .
cp README.md dist/displayctl/
tar czf "$TAR" -C dist displayctl

echo
echo "$TAR"
shasum -a 256 "$TAR"
