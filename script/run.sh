#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Use 2-second processing time in demo mode so CI completes quickly.
# Override by setting PROCESS_SECONDS in the environment.
export PROCESS_SECONDS="${PROCESS_SECONDS:-2}"

echo "Running demo (PROCESS_SECONDS=${PROCESS_SECONDS})..."

if [ -f "./qms" ]; then
    ./qms --demo 2>&1 | tee result.txt
else
    go run . --demo 2>&1 | tee result.txt
fi

echo ""
echo "Output written to result.txt"
