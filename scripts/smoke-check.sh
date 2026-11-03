#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="${SMOKE_HOST:-127.0.0.1}"
PORT="${SMOKE_PORT:-18070}"
TIMEOUT="${SMOKE_TIMEOUT:-30}"
BIN="${SMOKE_BINARY:-$ROOT/openserp}"
LOG="$(mktemp /tmp/openserp-smoke.XXXXXX)"

cleanup() {
  kill "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
}
fail() {
  echo "smoke-check: $1" >&2
  tail -n 30 "$LOG" >&2 || true
  exit 1
}

command -v go >/dev/null || fail "go not found in PATH"
command -v curl >/dev/null || fail "curl not found in PATH"

go build -o "$BIN" "$ROOT" || fail "build failed"

"$BIN" serve -a "$HOST" -p "$PORT" >"$LOG" 2>&1 &
PID=$!
trap cleanup EXIT INT TERM

for ((i = 0; i < TIMEOUT; i++)); do
  kill -0 "$PID" 2>/dev/null || fail "server exited before becoming healthy"
  if curl -fsS --max-time 2 "http://$HOST:$PORT/health" >/dev/null 2>&1; then
    echo "smoke-check: healthy at http://$HOST:$PORT/health"
    rm -f "$LOG"
    trap - EXIT INT TERM
    cleanup
    exit 0
  fi
  sleep 1
done

fail "server did not become healthy within ${TIMEOUT}s"
