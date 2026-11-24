#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d "${TMPDIR:-/tmp}/meter-icon-test.XXXXXXXX")
trap 'rm -rf "$tmp"' EXIT
./scripts/macos-icon.sh go/internal/gui/assets/meter.png "$tmp/Meter.icns"
iconutil -c iconset "$tmp/Meter.icns" -o "$tmp/Meter.iconset"
for size in 16 32 128 256 512; do
  for scale in 1 2; do
    suffix=""
    if [[ $scale == 2 ]]; then suffix="@2x"; fi
    image="$tmp/Meter.iconset/icon_${size}x${size}${suffix}.png"
    expected=$((size * scale))
    dimensions=$(sips -g pixelWidth -g pixelHeight "$image")
    grep -q "pixelWidth: $expected$" <<< "$dimensions"
    grep -q "pixelHeight: $expected$" <<< "$dimensions"
  done
done
echo 'macOS icon representations passed'
