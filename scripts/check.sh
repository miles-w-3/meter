#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

files=(contracts/openapi.generated.json go/internal/protocol/api.gen.go go/internal/protocol/testdata/frames.json)
before=$(mktemp -d)
trap 'rm -rf "$before"' EXIT
for file in "${files[@]}"; do cp "$file" "$before/$(basename "$file")"; done
./scripts/generate.sh
for file in "${files[@]}"; do cmp "$file" "$before/$(basename "$file")"; done
./node_modules/.bin/tsc --noEmit
if [[ $(uname -s) == Darwin ]]; then ./scripts/macos-icon-test.sh; fi
mkdir -p .bin
(
  cd go
  test -z "$(gofmt -l cmd internal)"
  go test ./...
  go vet ./...
  go build -buildvcs=false -o ../.bin/workspace ./cmd/workspace
)
node scripts/auth-test.mjs
node scripts/models-test.mjs
node scripts/claude-provider-test.mjs
node scripts/claude-inventory-test.mjs
node scripts/transcript-error-test.mjs
node scripts/search-test.mjs
node scripts/web-fetch-test.mjs
node scripts/file-diffs-test.mjs
node scripts/mermaid-catalog-test.mjs
node scripts/compaction-test.mjs
node scripts/tree-test.mjs
node scripts/terminal-layout-test.mjs
node scripts/integration.mjs
python3 scripts/tui-smoke.py
