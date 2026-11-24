#!/usr/bin/env bash
# Build a self-contained Apple Silicon app on an Apple Silicon Mac.
set -euo pipefail
cd "$(dirname "$0")/.."
repo=$PWD
if [[ $(uname -s) != Darwin || $(uname -m) != arm64 ]]; then
  echo 'Build this release on an Apple Silicon Mac.' >&2
  exit 1
fi
if [[ $# -gt 1 ]]; then
  echo "Usage: $0 [output-directory]" >&2
  exit 1
fi
for tool in "${BUN_EXE:-bun}" "${GO_EXE:-go}" "${CMAKE_EXE:-cmake}" pnpm git ditto sips iconutil node curl shasum; do
  command -v "$tool" >/dev/null || { echo "Missing build tool: $tool" >&2; exit 1; }
done
output=${1:-.bin}
mkdir -p "$output" .bin/release-deps
output=$(cd "$output" && pwd)
cache="$repo/.bin/release-deps"
stage=$(mktemp -d "${TMPDIR:-/tmp}/meter-macos.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
app="$stage/Meter.app"
macos="$app/Contents/MacOS"
resources="$app/Contents/Resources"
mkdir -p "$macos" "$resources/licenses"
"$repo/scripts/macos-icon.sh" "$repo/go/internal/gui/assets/meter.png" "$resources/Meter.icns"

parakeet_commit=2eac2e77d9c7ef8899f7a86a46e693ac5ac1a5cd
source_dir=${PARAKEET_SOURCE:-$cache/parakeet-cpp}
if [[ ! -d $source_dir/.git ]]; then
  git init -q "$source_dir"
  git -C "$source_dir" fetch --depth 1 https://github.com/mudler/parakeet.cpp.git "$parakeet_commit"
  git -C "$source_dir" checkout --detach FETCH_HEAD
  git -C "$source_dir" submodule update --init --depth 1 third_party/ggml
fi
if [[ $(git -C "$source_dir" rev-parse HEAD) != "$parakeet_commit" ]]; then
  echo "Expected parakeet.cpp commit $parakeet_commit" >&2
  exit 1
fi
"${CMAKE_EXE:-cmake}" -S "$source_dir" -B "$cache/parakeet-build-macos-arm64" \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DCMAKE_PROJECT_parakeet_cpp_INCLUDE="$repo/dictation/worker.cmake" \
  -DPARAKEET_BUILD_CLI=OFF -DPARAKEET_BUILD_SERVER=OFF \
  -DPARAKEET_WITH_CED=OFF -DPARAKEET_WITH_VOICEDETECT=OFF \
  -DGGML_NATIVE=OFF -DGGML_OPENMP=OFF -DBUILD_SHARED_LIBS=OFF
"${CMAKE_EXE:-cmake}" --build "$cache/parakeet-build-macos-arm64" --target meter-dictation --parallel "${BUILD_JOBS:-4}"
pnpm install --frozen-lockfile
node scripts/bun-oauth-test.mjs
install -m 755 "$cache/parakeet-build-macos-arm64/meter-dictation" "$macos/meter-dictation"
revision=$(git rev-parse HEAD)
dirty=false
if [[ -n $(git status --porcelain) ]]; then dirty=true; fi
"${BUN_EXE:-bun}" build server/main.ts --compile --minify-syntax --minify-whitespace \
  --define "METER_GIT_REVISION=\"$revision\"" --define "METER_GIT_DIRTY=$dirty" \
  --no-compile-autoload-dotenv --no-compile-autoload-bunfig --outfile "$macos/meter-server"
(
  cd go
  CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 "${GO_EXE:-go}" build -buildvcs=false \
    -ldflags="-s -w -X main.bundledRevision=$revision -X main.bundledDirty=$dirty" -o "$macos/meter" ./cmd/workspace
  mic_source=$("${GO_EXE:-go}" list -m -f '{{.Dir}}' github.com/gen2brain/malgo)
  cp "$mic_source/LICENSE" "$resources/licenses/malgo.txt"

  mygo_source=$("${GO_EXE:-go}" list -m -f '{{.Dir}}' github.com/egoist/mygo)
  ghostty_metadata=$(node --input-type=module - "$mygo_source/plugins/terminal/mygo-plugin.json" <<'JS'
import { readFileSync } from "node:fs";

const { libraries } = JSON.parse(readFileSync(process.argv[2], "utf8"));
const library = libraries.find(({ name }) => name === "libghostty-vt");
const asset = library?.files["darwin-arm64"];
const source = library?.source.match(/^https:\/\/github\.com\/ghostty-org\/ghostty\/tree\/([a-f0-9]{40})$/);
if (asset?.name !== "libghostty-vt.dylib" || !/^https:\/\/[^\s]+$/.test(asset.url)
    || !/^[a-f0-9]{64}$/.test(asset.sha256) || !source) {
  throw new Error("Invalid darwin-arm64 libghostty-vt entry in mygo's manifest");
}
console.log(asset.url, asset.sha256, source[1]);
JS
  )
  read -r ghostty_url ghostty_sha ghostty_commit <<< "$ghostty_metadata"
  ghostty_dylib="$stage/libghostty-vt.dylib"
  curl -fLsS --retry 2 "$ghostty_url" -o "$ghostty_dylib"
  printf '%s  %s\n' "$ghostty_sha" "$ghostty_dylib" | shasum -a 256 -c -
  install -m 644 "$ghostty_dylib" "$resources/libghostty-vt.dylib"
  curl -fLsS --retry 2 "https://raw.githubusercontent.com/ghostty-org/ghostty/$ghostty_commit/LICENSE" \
    -o "$resources/licenses/ghostty.txt"
)
cp "$source_dir/LICENSE" "$resources/licenses/parakeet-cpp.txt"
cp "$source_dir/third_party/ggml/LICENSE" "$resources/licenses/ggml.txt"
cp go/internal/gui/fonts/*-OFL.txt "$resources/licenses/"
cp go/internal/gui/fonts/SymbolsNerdFont-{LICENSE.txt,README.md,SOURCE.txt,License-Audit.md} "$resources/licenses/"
cat > "$app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>meter</string>
  <key>CFBundleIdentifier</key><string>local.meter.app</string>
  <key>CFBundleName</key><string>Meter</string>
  <key>CFBundleDisplayName</key><string>Meter</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>Meter</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSMicrophoneUsageDescription</key><string>Meter uses the microphone for local dictation.</string>
</dict></plist>
PLIST
# Go's module cache can contain read-only files, and cp preserves their mode.
# Keep the entire personal-build bundle user-writable so its owner can remove
# quarantine recursively after downloading it. Preserve executable bits.
chmod -R u+w "$app"
# ditto preserves the bundle and executable permissions when transferred.
archive="$output/meter-macos-arm64.zip"
rm -f "$archive"
ditto -c -k --keepParent "$app" "$archive"
echo "Built $archive"
