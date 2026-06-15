#!/usr/bin/env bash
set -e

export PATH=$PATH:/usr/local/go/bin:/home/molla/.bun/bin
cd ~/new-api

source .env.test

echo "=============================="
echo "  1. Go unit/integration tests"
echo "=============================="
go test ./controller/... ./service/... -count=1 "$@"

echo ""
echo "=============================="
echo "  2. Frontend unit tests"
echo "=============================="
cd web/default
bun test

echo ""
echo "=============================="
echo "  3. E2E tests (Playwright)"
echo "=============================="
source ~/new-api/.env.test
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium-browser \
  bunx playwright test
