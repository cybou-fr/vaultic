#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist
go mod tidy
go build -trimpath -ldflags='-s -w' -o dist/vaultic ./cmd/vaultic
cp portable.flag LICENSE NOTICE THIRD_PARTY_NOTICES.md dist/
printf 'Built dist/vaultic (add restic executable to PATH)\n'
