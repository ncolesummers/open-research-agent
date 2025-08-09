#!/usr/bin/env bash
# Simple wrapper to add emojis to `go test` output
set -euo pipefail

go test "$@" | sed -E \
  -e 's/^([[:space:]]*)(--- PASS:.*)/\1✅ \2/g' \
  -e 's/^([[:space:]]*)(--- SKIP:.*)/\1⏭️ \2/g' \
  -e 's/^PASS/✅ PASS/g' \
  -e 's/^FAIL/❌ FAIL/g' \
  -e 's/^ok /✅ ok /g'

