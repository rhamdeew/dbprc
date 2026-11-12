#!/usr/bin/env bash
# Build the frontend, start mockoidc and the API with a temp database, run Playwright.
# No Docker. Ports: mockoidc 127.0.0.1:9999 (fixed) and the API on E2E_PORT (default 18080).
# Stop `make dev` first; it already binds 9999.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

port="${E2E_PORT:-18080}"
base="http://127.0.0.1:${port}"
mock_origin="http://127.0.0.1:9999"
tmpdir=$(mktemp -d)
mock_pid=""
api_pid=""

cleanup() {
  local status=$?
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" 2>/dev/null || true
  fi
  if [[ -n "$mock_pid" ]]; then
    kill "$mock_pid" 2>/dev/null || true
  fi
  if [[ -n "$api_pid" ]]; then
    wait "$api_pid" 2>/dev/null || true
  fi
  if [[ -n "$mock_pid" ]]; then
    wait "$mock_pid" 2>/dev/null || true
  fi
  if [[ "$status" -ne 0 ]]; then
    echo "---- mockoidc ----" >&2
    tail -n 80 "$tmpdir/mock.log" >&2 || true
    echo "---- api ----" >&2
    tail -n 120 "$tmpdir/api.log" >&2 || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

if curl -sf -o /dev/null --max-time 1 "$mock_origin/oidc/.well-known/openid-configuration"; then
  echo "127.0.0.1:9999 is already in use. Stop make dev, then run make e2e again." >&2
  exit 1
fi
if curl -sf -o /dev/null --max-time 1 "$base/healthz"; then
  echo "${base} is already in use. Set E2E_PORT to a free port and run make e2e again." >&2
  exit 1
fi

echo "building frontend"
(cd frontend && npm run build)

if [[ "${CI:-}" == "true" || ! -d e2e/node_modules/@playwright/test ]]; then
  (cd e2e && npm ci --no-audit --no-fund)
fi
if [[ "${CI:-}" == "true" ]]; then
  (cd e2e && npx playwright install --with-deps chromium)
else
  (cd e2e && npx playwright install chromium)
fi

mkdir -p "$tmpdir/data" "$tmpdir/uploads"
echo "building api and mockoidc"
(
  cd backend
  go build -o "$tmpdir/arc" ./cmd/arc
  go build -o "$tmpdir/mockoidc" ./cmd/mockoidc
)

"$tmpdir/mockoidc" >"$tmpdir/mock.log" 2>&1 &
mock_pid=$!

MESSAGE_KEY="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" \
  MESSAGE_KEY_OLD="" \
  APP_ENV=dev \
  PORT="$port" \
  DATA_DIR="$tmpdir/data" \
  UPLOAD_DIR="$tmpdir/uploads" \
  STATIC_DIR="$root/frontend/dist" \
  APP_BASE_URL="$base" \
  OIDC_ISSUER="http://localhost:9999/oidc" \
  OIDC_CLIENT_ID="arc-dev" \
  OIDC_CLIENT_SECRET="arc-dev-secret" \
  ALLOWED_DOMAIN="example.com" \
  ADMIN_EMAILS="admin@example.com" \
  SESSION_COOKIE="arc_sid" \
  "$tmpdir/arc" >"$tmpdir/api.log" 2>&1 &
api_pid=$!

wait_url() {
  local url=$1
  local i
  for i in $(seq 1 60); do
    if curl -sf -o /dev/null --max-time 1 "$url"; then
      return 0
    fi
    if ! kill -0 "$mock_pid" 2>/dev/null || ! kill -0 "$api_pid" 2>/dev/null; then
      echo "a server exited while waiting for $url" >&2
      return 1
    fi
    sleep 0.5
  done
  echo "timed out waiting for $url" >&2
  return 1
}

wait_url "$mock_origin/oidc/.well-known/openid-configuration"
wait_url "$base/healthz"

export E2E_BASE_URL="$base"
export E2E_MOCK_ORIGIN="$mock_origin"
cd e2e
npm run typecheck
npx playwright test
