#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

echo "══════════════════════════════════════"
echo " Running unit tests"
echo "══════════════════════════════════════"
go test ./internal/order/... ./internal/queue/... -v -race -timeout 30s

echo ""
echo "══════════════════════════════════════"
echo " Running controller unit tests"
echo "══════════════════════════════════════"
go test ./internal/controller/... -v -race -run "^Test[^L]" -timeout 60s

echo ""
echo "══════════════════════════════════════"
echo " Running load tests"
echo "══════════════════════════════════════"
go test ./internal/controller/... -v -race -run "^TestLoad" -timeout 120s

echo ""
echo "All tests passed ✓"
