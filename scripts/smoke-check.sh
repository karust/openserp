#!/usr/bin/env bash
# Release smoke check: build, start the server, wait for /health, then stop.
# Exits non-zero if the server never becomes healthy.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BIN="${BIN:-./openserp}"
PORT="${PORT:-7000}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-30}"
HEALTH_URL="http://127.0.0.1:${PORT}/health"

echo "==> Building binary (make build)"
make build
if [[ ! -x "$BIN" ]]; then
  echo "error: binary not found at '$BIN' after 'make build'" >&2
  exit 1
fi

echo "==> Starting server: $BIN serve"
"$BIN" serve &
SERVER_PID=$!
cleanup() {
  kill "$SERVER_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> Waiting for $HEALTH_URL (timeout ${HEALTH_TIMEOUT}s)"
deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
while true; do
  if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "error: server exited before becoming healthy" >&2
    exit 1
  fi
  if (( $(date +%s) >= deadline )); then
    echo "error: server did not become healthy within ${HEALTH_TIMEOUT}s" >&2
    exit 1
  fi
  sleep 1
done
echo "==> Server is healthy"
echo "Smoke check passed"
