#!/usr/bin/env bash
# shellcheck disable=SC2317 # cleanup is invoked by an EXIT trap
#
# Unit tests for ci/mosaic/start.sh, ci/mosaic/stop.sh and ci/mosaic/get_health.sh.
#
# The scripts are exercised against a small local fake HTTP server that records
# the requests it receives, so no Docker stand is required.
#
# Usage: ./ci/mosaic/test_rest.sh

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

require_command python3
require_command curl

passed=0
failed=0

ok() {
  passed=$((passed + 1))
  printf 'ok - %s\n' "$*"
}

not_ok() {
  failed=$((failed + 1))
  printf 'not ok - %s\n' "$*" >&2
}

# ---------------------------------------------------------------------------
# Fake HTTP server standing in for MosaicComposer.
#
# It records every request line ("METHOD PATH") to $REQUEST_LOG and answers
# with fixed bodies matching the real endpoints.
# ---------------------------------------------------------------------------
readonly REQUEST_LOG="$REPO_ROOT/.cache/test-rest-requests.log"
readonly REQUEST_BODY_LOG="$REPO_ROOT/.cache/test-rest-bodies.log"
readonly RESPONSE_DIR="$REPO_ROOT/.cache/test-rest-responses"

fake_server() {
  local port="$1"
  # Replace the background subshell with the Python process so that $! is the
  # server PID and the EXIT trap can always stop it.
  exec python3 - "$port" "$REQUEST_LOG" "$REQUEST_BODY_LOG" "$RESPONSE_DIR" <<'PY'
import http.server
import json
import os
import sys

port = int(sys.argv[1])
request_log = sys.argv[2]
request_body_log = sys.argv[3]
response_dir = sys.argv[4]

os.makedirs(response_dir, exist_ok=True)

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return  # keep stdout quiet

    def _record(self, body=None):
        with open(request_log, "a") as handle:
            handle.write("%s %s\n" % (self.command, self.path))
        if body is not None:
            with open(request_body_log, "a") as handle:
                handle.write("%s %s %s\n" % (self.command, self.path, body))

    def do_GET(self):
        self._record()
        path = os.path.join(response_dir, "health.json")
        with open(path, "rb") as handle:
            body = handle.read()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length).decode()
        self._record(body)
        status = "stopped"
        if self.path == "/start":
            status = "started"
        body = json.dumps({"status": status}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
}

# Find a free port.
free_port() {
  python3 - <<'PY'
import socket
with socket.socket() as s:
    s.bind(("127.0.0.1", 0))
    print(s.getsockname()[1])
PY
}

cleanup() {
  if [[ -n "${fake_pid:-}" ]]; then
    kill "$fake_pid" 2>/dev/null || true
    wait "$fake_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

port="$(free_port)"
rm -f "$REQUEST_LOG" "$REQUEST_BODY_LOG"
mkdir -p "$RESPONSE_DIR"

cat >"$RESPONSE_DIR/health.json" <<'JSON'
{
  "status": "ok",
  "subsystems": {
    "livekit": {
      "status": "ok",
      "connected": true,
      "url": "ws://127.0.0.1:7880",
      "room": "mosaic-composer",
      "identity": "mosaic-composer",
      "connection_state": "connected"
    },
    "mosaic": {
      "state": "running",
      "participants": 2,
      "rotation_timeout_seconds": 4,
      "grid": {
        "columns": 5,
        "rows": 2
      }
    }
  }
}
JSON

export MOSAIC_URL="http://127.0.0.1:$port"
fake_server "$port" &
fake_pid=$!

# Give the server a moment to start listening.
for _ in $(seq 1 50); do
  if curl -fsS -o /dev/null "http://127.0.0.1:$port/health" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

run_script() {
  local name="$1"
  shift
  set +e
  "$MOSAIC_DIR/$name.sh" "$@" >"$REPO_ROOT/.cache/test-rest-last.out" 2>&1
  local rc=$?
  set -e
  if [[ $rc -ne 0 ]]; then
    not_ok "$name.sh exited with code $rc"
    printf '%s\n' "$(cat "$REPO_ROOT/.cache/test-rest-last.out")" >&2
    return 1
  fi
  return 0
}

run_script_expect_failure() {
  local name="$1"
  shift
  set +e
  "$MOSAIC_DIR/$name.sh" "$@" >"$REPO_ROOT/.cache/test-rest-last.out" 2>&1
  local rc=$?
  set -e
  if [[ $rc -eq 0 ]]; then
    not_ok "$name.sh succeeded but was expected to fail"
    return 1
  fi
  return 0
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  if printf '%s' "$haystack" | grep -qF -- "$needle"; then
    ok "$label"
  else
    not_ok "$label (output: $haystack)"
  fi
}

assert_request() {
  local method="$1"
  local path="$2"
  local label="$3"
  if grep -qxF "$method $path" "$REQUEST_LOG"; then
    ok "$label"
  else
    not_ok "$label (requests: $(tr '\n' ' ' 2>/dev/null < "$REQUEST_LOG"))"
  fi
}

assert_request_body() {
  local method="$1"
  local path="$2"
  local body="$3"
  local label="$4"
  if grep -qxF "$method $path $body" "$REQUEST_BODY_LOG"; then
    ok "$label"
  else
    not_ok "$label (bodies: $(tr '\n' ' ' 2>/dev/null < "$REQUEST_BODY_LOG"))"
  fi
}

# ---------------- start.sh ----------------
run_script start
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "POST /start returns started status"
assert_request "POST" "/start" "start.sh called POST /start"

run_script start 5x2
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts x-separated grid dimensions"
assert_request_body "POST" "/start" '{"columns":5,"rows":2}' \
  "start.sh sends x-separated grid dimensions in the JSON body"

run_script start "$MOSAIC_URL" 6:1
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts colon-separated grid dimensions"
assert_request_body "POST" "/start" '{"columns":6,"rows":1}' \
  "start.sh sends colon-separated grid dimensions in the JSON body"

run_script start 010x02
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts grid dimensions with leading zeroes"
assert_request_body "POST" "/start" '{"columns":10,"rows":2}' \
  "start.sh normalizes leading-zero grid dimensions to decimal"

run_script start 08x2
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh handles leading-zero values that look like octal"
assert_request_body "POST" "/start" '{"columns":8,"rows":2}' \
  "start.sh sends octal-looking grid dimensions as decimal"

run_script_expect_failure start 5x
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "Invalid grid dimensions" "start.sh rejects a malformed grid"

run_script_expect_failure start 0x2
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "positive" "start.sh rejects non-positive grid dimensions"

run_script start --rotation-timeout 3
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts a rotation timeout without a grid"
assert_request_body "POST" "/start" '{"rotation_timeout_seconds":3}' \
  "start.sh sends a rotation timeout without a grid"

run_script start 3x2 --rotation-timeout 7
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts a grid and rotation timeout"
assert_request_body "POST" "/start" '{"columns":3,"rows":2,"rotation_timeout_seconds":7}' \
  "start.sh sends a grid and rotation timeout"

run_script start --rotation-timeout 5 4x1
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts a rotation timeout before a grid"
assert_request_body "POST" "/start" '{"columns":4,"rows":1,"rotation_timeout_seconds":5}' \
  "start.sh sends a rotation timeout before a grid"

run_script start "$MOSAIC_URL" --rotation-timeout 9
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts a URL and rotation timeout"
assert_request_body "POST" "/start" '{"rotation_timeout_seconds":9}' \
  "start.sh sends a URL and rotation timeout"

run_script_expect_failure start --rotation-timeout 0
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "rotation timeout" "start.sh rejects a zero rotation timeout"

run_script_expect_failure start --rotation-timeout -1
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "rotation timeout" "start.sh rejects a negative rotation timeout"

run_script_expect_failure start --rotation-timeout abc
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "rotation timeout" "start.sh rejects a non-numeric rotation timeout"

run_script_expect_failure start --rotation-timeout
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "rotation timeout" "start.sh rejects a missing rotation timeout value"

run_script_expect_failure start --rotation-timeout 3 --rotation-timeout 4
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "rotation timeout specified more than once" \
  "start.sh rejects a duplicate rotation timeout"

# ---------------- stop.sh ----------------
run_script stop
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "stopped" "POST /stop returns stopped status"
assert_request "POST" "/stop" "stop.sh called POST /stop"

# ---------------- get_health.sh ----------------
run_script get_health
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" '"status": "ok"' "get_health prints health status"
assert_contains "$out" '"connected": true' "get_health prints LiveKit connection"
assert_contains "$out" "mosaic-composer" "get_health prints room name"
assert_contains "$out" '"state": "running"' "get_health prints mosaic state"
assert_contains "$out" '"participants": 2' "get_health prints participant count"
assert_contains "$out" '"rotation_timeout_seconds": 4' "get_health prints rotation timeout while running"
assert_contains "$out" '"columns": 5' "get_health prints grid columns while running"
assert_contains "$out" '"rows": 2' "get_health prints grid rows while running"
assert_request "GET" "/health" "get_health called GET /health"

# Human-readable formatting: jq output should be indented.
if printf '%s' "$out" | grep -qE '^\s{2}"status"'; then
  ok "get_health output is indented (human-readable)"
else
  not_ok "get_health output is indented (human-readable)"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
