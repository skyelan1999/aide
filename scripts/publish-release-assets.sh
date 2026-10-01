#!/usr/bin/env bash
# Create or update a prerelease for an already-pushed, versioned tag and upload assets.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
TAG="${1:-}"
ASSET_DIR="${2:-}"
[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+-RC[0-9]+$ ]] || { echo 'Usage: scripts/publish-release-assets.sh <vX.Y.Z.W-RCn> <asset-directory>' >&2; exit 2; }
[[ -d "$ASSET_DIR" ]] || { echo "Asset directory not found: $ASSET_DIR" >&2; exit 1; }
gh auth status >/dev/null
git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null || { echo "Tag $TAG is not present on origin; push the tag before publishing." >&2; exit 1; }
shopt -s nullglob
ASSETS=("$ASSET_DIR"/*.tar.gz "$ASSET_DIR"/*.zip "$ASSET_DIR"/SHA256SUMS "$ASSET_DIR"/RELEASE-ASSETS.txt)
(( ${#ASSETS[@]} >= 5 )) || { echo "Expected image, launcher ZIPs, checksum, and asset note in $ASSET_DIR" >&2; exit 1; }
for file in "${ASSETS[@]}"; do [[ -s "$file" ]] || { echo "Empty asset: $file" >&2; exit 1; }; done
if gh release view "$TAG" >/dev/null 2>&1; then
  echo "Release $TAG already exists; uploading assets without replacing existing names."
  gh release upload "$TAG" "${ASSETS[@]}"
else
  NOTES="$ROOT/docs/reviews/2026-10-01-v0.1.14.0-RC2.md"
  gh release create "$TAG" "${ASSETS[@]}" --verify-tag --prerelease \
    --title "aide $TAG" --notes-file "$NOTES"
fi
gh release view "$TAG" --json tagName,name,isPrerelease,url,assets
