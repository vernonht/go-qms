#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Building qms..."
go build -o qms .
echo "Build successful → ./qms"
