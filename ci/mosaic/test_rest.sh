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
readonly RESPONSE_DIR="$REPO_ROOT/.cache/test-rest-responses"

fake_server() {
  local port="$1"
  # Replace the background subshell with the Python process so that $! is the
  # server PID and the EXIT trap can always stop it.
  exec python3 - "$port" "$REQUEST_LOG" "$RESPONSE_DIR" <<'PY'
import http.server
import json
import os
import sys

port = int(sys.argv[1])
request_log = sys.argv[2]
response_dir = sys.argv[3]

os.makedirs(response_dir, exist_ok=True)

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return  # keep stdout quiet

    def _record(self):
        with open(request_log, "a") as handle:
            handle.write("%s %s\n" % (self.command, self.path))

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
        self._record()
        length = int(self.headers.get("Content-Length", 0))
        self.rfile.read(length)
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
rm -f "$REQUEST_LOG"
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
      "participants": 2
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

# ---------------- start.sh ----------------
run_script start
out="$(cat "$REPO_ROOT/.cache/test-rest-last.out")"
assert_contains "$out" "started" "POST /start returns started status"
assert_request "POST" "/start" "start.sh called POST /start"

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
assert_request "GET" "/health" "get_health called GET /health"

# Human-readable formatting: jq output should be indented.
if printf '%s' "$out" | grep -qE '^\s{2}"status"'; then
  ok "get_health output is indented (human-readable)"
else
  not_ok "get_health output is indented (human-readable)"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
