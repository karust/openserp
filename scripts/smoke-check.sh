#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="${SMOKE_HOST:-127.0.0.1}"
PORT="${SMOKE_PORT:-18070}"
TIMEOUT="${SMOKE_TIMEOUT:-30}"
BIN="${SMOKE_BINARY:-}"
HEALTH_URL="http://$HOST:$PORT/health"
CURL=(curl --disable --noproxy '*' --silent --max-time 1 --output /dev/null)
PID=""
LOG=""

cleanup() {
  if [[ -n "$PID" ]]; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -f -- "$LOG" "$WORK_DIR/openserp" "$WORK_DIR/openserp.exe"
  rmdir -- "$WORK_DIR"
}
fail() {
  echo "smoke-check: $1" >&2
  if [[ -f "$LOG" ]]; then
    tail -n 30 "$LOG" >&2
  fi
  exit 1
}
check_port() {
  local connected_port
  # A TCP listener can occupy the port without serving /health.
  connected_port=$("${CURL[@]}" --write-out '%{remote_port}' "$HEALTH_URL" || true)
  if [[ -n "$connected_port" && "$connected_port" != 0 ]]; then
    fail "port $PORT is already in use on $HOST"
  fi
}

command -v curl >/dev/null || fail "curl not found in PATH"
[[ "$PORT" =~ ^[1-9][0-9]{0,4}$ ]] && (( PORT <= 65535 )) || fail "SMOKE_PORT must be between 1 and 65535"
[[ "$TIMEOUT" =~ ^[1-9][0-9]*$ ]] || fail "SMOKE_TIMEOUT must be a positive integer"
check_port

TMP_ROOT=$(cd -- "${TMPDIR:-/tmp}" && pwd -P) || fail "could not open the temporary directory"
WORK_DIR=$(mktemp -d "$TMP_ROOT/openserp-smoke.XXXXXX") || fail "could not create a temporary directory"
LOG="$WORK_DIR/server.log"
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ -n "$BIN" ]]; then
  case "$BIN" in
    /* | [[:alpha:]]:[\/]*) ;;
    *) BIN="$PWD/$BIN" ;;
  esac
  [[ -f "$BIN" && -x "$BIN" ]] || fail "SMOKE_BINARY is not executable: $BIN"
else
  command -v go >/dev/null || fail "go not found in PATH"
  GO_EXE=$(go env GOEXE) || fail "could not determine the binary extension"
  BIN="$WORK_DIR/openserp$GO_EXE"
  go -C "$ROOT" build -o "$BIN" . || fail "build failed"
  check_port
fi

OPENSERP_APP_SAVE_LOG=false "$BIN" serve -a "$HOST" -p "$PORT" >"$LOG" 2>&1 &
PID=$!

deadline=$((SECONDS + TIMEOUT))
while kill -0 "$PID" 2>/dev/null; do
  if "${CURL[@]}" --fail "$HEALTH_URL" && kill -0 "$PID" 2>/dev/null; then
    echo "smoke-check: healthy at $HEALTH_URL"
    exit 0
  fi
  if (( SECONDS >= deadline )); then
    fail "server did not become healthy within ${TIMEOUT}s"
  fi
  sleep 1
done

fail "server exited before becoming healthy"
