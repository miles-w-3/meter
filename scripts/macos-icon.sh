#!/usr/bin/env bash
# Compile a macOS app icon from the square, 1024px source artwork.
set -euo pipefail
if [[ $# -ne 2 ]]; then
  echo "Usage: $0 source.png destination.icns" >&2
  exit 1
fi
source_png=$1
output=$2
stage=$(mktemp -d "${TMPDIR:-/tmp}/meter-icon.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
iconset="$stage/Meter.iconset"
mkdir -p "$iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$source_png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$source_png" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$output"
