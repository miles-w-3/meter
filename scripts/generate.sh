#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
node contracts/generate.ts
cd go
go tool oapi-codegen --config internal/protocol/codegen.yaml ../contracts/openapi.generated.json
