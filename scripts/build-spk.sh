#!/usr/bin/env bash
# Build a Synology .spk for the DS215j (Marvell Armada 375 / ARMv7).
#
# Usage:
#   scripts/build-spk.sh [VERSION]
# If VERSION is omitted, defaults to "1.0.1-$(date +%Y%m%d)".
#
# Output: dist/SoundTouchBridge-<VERSION>-armada375.spk

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="${1:-1.0.1-$(date +%Y%m%d)}"
PKG_NAME="SoundTouchBridge"
ARCH="armada375"
OUTPUT="dist/${PKG_NAME}-${VERSION}-${ARCH}.spk"

PACKAGING="$REPO_ROOT/packaging/synology"
STAGE="$(mktemp -d -t soundtouch-spk-XXXXXX)"
trap 'rm -rf "$STAGE"' EXIT

echo "==> Cross-compiling bridge for linux/arm/v7"
mkdir -p "$STAGE/payload/bin" "$STAGE/payload/etc" "$STAGE/payload/var" "$STAGE/spk/scripts"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "$STAGE/payload/bin/bridge" .
chmod 755 "$STAGE/payload/bin/bridge"

file "$STAGE/payload/bin/bridge"

echo "==> Staging payload"
cp "$PACKAGING/config.yaml.example" "$STAGE/payload/etc/config.yaml.example"

echo "==> Building package.tgz"
tar -C "$STAGE/payload" -czf "$STAGE/spk/package.tgz" .

echo "==> Rendering INFO (version=${VERSION})"
sed "s/__VERSION__/${VERSION}/" "$PACKAGING/INFO.template" > "$STAGE/spk/INFO"

echo "==> Copying lifecycle scripts"
cp "$PACKAGING/scripts/"* "$STAGE/spk/scripts/"
chmod 755 "$STAGE/spk/scripts/"*

echo "==> Assembling .spk"
mkdir -p "$REPO_ROOT/dist"
tar -C "$STAGE/spk" -cf "$REPO_ROOT/$OUTPUT" .

echo
echo "Built $OUTPUT"
ls -lh "$REPO_ROOT/$OUTPUT"
