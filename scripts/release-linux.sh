#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
repo=$PWD
model_only=false
if [[ ${1:-} == --model-only ]]; then
  model_only=true
  shift
fi
if [[ $# -gt 1 || ${1:-} == -* ]]; then
  printf 'Usage: %s [--model-only] [output-directory]\n' "$0" >&2
  exit 1
fi
if [[ $model_only == false && ( $(uname -s) != Linux || $(uname -m) != x86_64 ) ]]; then
  printf '%s\n' 'This release script builds on Linux x86-64.' >&2
  exit 1
fi

output=${1:-.bin}
mkdir -p "$output" .bin/release-deps
output=$(realpath "$output")
cache="$repo/.bin/release-deps"
stage=$(mktemp -d "$output/.meter-release.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT

package() {
  local name=$1
  tar --sort=name --mtime='@0' --owner=0 --group=0 -C "$stage" -cf - meter-linux-x86_64 \
    | gzip -n -6 > "$stage/$name"
  mv "$stage/$name" "$output/$name"
  printf '%s\n' "$output/$name"
}

folder="$stage/meter-linux-x86_64"
mkdir -p "$folder/licenses"

if [[ $model_only == true ]]; then
  model_commit=07a08a3a405d20c5434b9a1442f6e6cf571f57ad
  model_hash=62616b914d6f5a683a5dea672df055b57de5c49dddf871b8b44b9c814dc3d896
  model_file=$(realpath -m "${PARAKEET_MODEL:-$cache/parakeet-eou-q8.gguf}")
  nvidia_notice=$(realpath -m "${NVIDIA_LICENSE_HTML:-$cache/nvidia-open-model-license.html}")

  if [[ ! -f $model_file ]]; then
    curl -fLsS --retry 2 "https://huggingface.co/mudler/parakeet-cpp-gguf/resolve/$model_commit/realtime_eou_120m-v1-q8_0.gguf?download=true" -o "$stage/model"
    printf '%s  %s\n' "$model_hash" "$stage/model" | sha256sum --check
    mv "$stage/model" "$model_file"
  fi
  printf '%s  %s\n' "$model_hash" "$model_file" | sha256sum --check
  if [[ ! -f $nvidia_notice ]]; then
    curl -fLsS --retry 2 'https://www.nvidia.com/en-us/agreements/enterprise-software/nvidia-open-model-license/' -o "$stage/nvidia-license"
    mv "$stage/nvidia-license" "$nvidia_notice"
  fi

  install -m 644 "$model_file" "$folder/parakeet-eou-q8.gguf"
  python3 - "$nvidia_notice" "$folder/licenses/NVIDIA-Open-Model-License.html" <<'PYTHON'
from pathlib import Path
import sys

page = Path(sys.argv[1]).read_text()
start = page.index("<p>This NVIDIA Open Model License Agreement")
end = page.index("</div>", start)
agreement = page[start:end]
if "If you distribute the Model" not in agreement or "<script" in agreement.lower():
    raise SystemExit("Could not extract the NVIDIA Open Model License")
Path(sys.argv[2]).write_text(
    '<!doctype html>\n<meta charset="utf-8">\n<title>NVIDIA Open Model License</title>\n'
    '<h1>NVIDIA Open Model License Agreement</h1>\n' + agreement
)
PYTHON
  cat > "$folder/MODEL-NOTICE.txt" <<'NOTICE'
Licensed by NVIDIA Corporation under the NVIDIA Open Model License
Model: NVIDIA Parakeet Realtime EOU 120M v1, converted to GGUF and
quantized to Q8_0 by the parakeet.cpp project.
https://huggingface.co/nvidia/parakeet_realtime_eou_120m-v1
https://huggingface.co/mudler/parakeet-cpp-gguf
NOTICE
  package meter-parakeet-q8-model.tar.gz
  exit 0
fi

parakeet_commit=2eac2e77d9c7ef8899f7a86a46e693ac5ac1a5cd
source_dir=$(realpath -m "${PARAKEET_SOURCE:-$cache/parakeet-cpp}")

if [[ ! -d $source_dir/.git ]]; then
  git init -q "$source_dir"
  git -C "$source_dir" fetch --depth 1 https://github.com/mudler/parakeet.cpp.git "$parakeet_commit"
  git -C "$source_dir" checkout --detach FETCH_HEAD
  git -C "$source_dir" submodule update --init --depth 1 third_party/ggml
fi
if [[ $(git -C "$source_dir" rev-parse HEAD) != "$parakeet_commit" ]]; then
  printf 'Expected parakeet.cpp commit %s\n' "$parakeet_commit" >&2
  exit 1
fi
"${CMAKE_EXE:-cmake}" -S "$source_dir" -B "$cache/parakeet-build" \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_PROJECT_parakeet_cpp_INCLUDE="$repo/dictation/worker.cmake" \
  -DPARAKEET_BUILD_CLI=OFF -DPARAKEET_BUILD_SERVER=OFF \
  -DPARAKEET_WITH_CED=OFF -DPARAKEET_WITH_VOICEDETECT=OFF \
  -DGGML_NATIVE=OFF -DGGML_OPENMP=OFF -DBUILD_SHARED_LIBS=OFF
"${CMAKE_EXE:-cmake}" --build "$cache/parakeet-build" --target meter-dictation --parallel "${BUILD_JOBS:-4}"
pnpm install --frozen-lockfile

install -m 755 "$cache/parakeet-build/meter-dictation" "$folder/meter-dictation"
strip --strip-unneeded "$folder/meter-dictation"
revision=$(git rev-parse HEAD)
dirty=false
if [[ -n $(git status --porcelain) ]]; then dirty=true; fi
"${BUN_EXE:-bun}" build server/main.ts --compile --minify-syntax --minify-whitespace \
  --define "METER_GIT_REVISION=\"$revision\"" --define "METER_GIT_DIRTY=$dirty" \
  --no-compile-autoload-dotenv --no-compile-autoload-bunfig --outfile "$folder/meter-server"
(
  cd go
  CGO_ENABLED=1 "${GO_EXE:-go}" build -buildvcs=false -ldflags="-s -w -X main.bundledRevision=$revision -X main.bundledDirty=$dirty" -o "$folder/workspace" ./cmd/workspace
  mic_source=$("${GO_EXE:-go}" list -m -f '{{.Dir}}' github.com/gen2brain/malgo)
  cp "$mic_source/LICENSE" "$folder/licenses/malgo.txt"
)
cp "$source_dir/LICENSE" "$folder/licenses/parakeet-cpp.txt"
cp "$source_dir/third_party/ggml/LICENSE" "$folder/licenses/ggml.txt"
cat > "$folder/README.txt" <<'README'
Meter for Linux x86-64

Run ./workspace gui with your usual Meter provider settings.

Click the daemon badge to see the running and bundled revisions, restart
the daemon, redeploy the bundled binary, or download the dictation model.
Meter contacts Hugging Face only when you choose to download the model.

For an offline install, extract the separate meter-parakeet-q8-model.tar.gz
archive into the same parent directory as this archive. This places the
model beside workspace, meter-server, and meter-dictation. Refresh the
Backend panel to pick up the change.

Hold "Hold to dictate" in a pane and release to insert the transcript.
Recognition runs locally. Your conversation provider is configured separately.

To transcribe a file of mono 16 kHz signed 16-bit little-endian PCM:
  PI_PROVIDER=fixture ./workspace dictation < audio.pcm

The inference worker was built for x86-64 CPUs with AVX2 and the
release uses system libraries for Linux desktop and audio support.
README
package meter-linux-x86_64-app.tar.gz
