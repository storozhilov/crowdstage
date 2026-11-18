#!/usr/bin/env bash
# shellcheck disable=SC2317 # cleanup is invoked by an EXIT trap
#
# Unit tests for ci/media-mixer/start.sh, ci/media-mixer/stop.sh,
# ci/media-mixer/switch_source.sh, and ci/media-mixer/get_health.sh.
#
# The scripts are exercised against a small local fake HTTP server that records
# the requests it receives, so no Docker stand is required.
#
# Usage: ./ci/media-mixer/test_rest.sh

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

require_command python3
require_command curl
require_command jq

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

readonly REQUEST_LOG="$REPO_ROOT/.cache/media-mixer-test-rest-requests.log"
readonly RESPONSE_DIR="$REPO_ROOT/.cache/media-mixer-test-rest-responses"

fake_server() {
  local port="$1"
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
        return

    def _record(self):
        with open(request_log, "a") as handle:
            handle.write("%s %s\n" % (self.command, self.path))

    def _record_body(self, body):
        with open(request_log, "a") as handle:
            handle.write("BODY %s\n" % body.decode())

    def do_GET(self):
        self._record()
        with open(os.path.join(response_dir, "health.json"), "rb") as handle:
            body = handle.read()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        self._record()
        content_length = int(self.headers.get("Content-Length", "0"))
        if content_length:
            self._record_body(self.rfile.read(content_length))
        status = "stopped"
        if self.path == "/start":
            status = "started"
        elif self.path == "/switch":
            status = "switched"
        body = json.dumps({"status": status}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
}

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
    "mixer": {
      "state": "running",
      "inputs": 0,
      "outputs": 1
    }
  }
}
JSON

export MEDIA_MIXER_URL="http://127.0.0.1:$port"
fake_server "$port" &
fake_pid=$!

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
  "$MEDIA_MIXER_DIR/$name.sh" "$@" >"$REPO_ROOT/.cache/media-mixer-test-rest-last.out" 2>&1
  local rc=$?
  set -e
  if [[ $rc -ne 0 ]]; then
    not_ok "$name.sh exited with code $rc"
    printf '%s\n' "$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")" >&2
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

run_script start
out="$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")"
assert_contains "$out" "started" "POST /start returns started status"
assert_request "POST" "/start" "start.sh called POST /start"

run_script start --guest-broadcast-seconds 15
out="$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")"
assert_contains "$out" "started" "start.sh accepts a guest broadcast duration"
body_line="$(grep '^BODY ' "$REQUEST_LOG" | tail -n1 | sed 's/^BODY //')"
if [[ "$(printf '%s' "$body_line" | jq -r '.guest_broadcast_seconds')" == "15" ]]; then
  ok "start.sh sends the guest broadcast duration"
else
  not_ok "start.sh sends the guest broadcast duration (body: $body_line)"
fi

set +e
"$MEDIA_MIXER_DIR/start.sh" --guest-broadcast-seconds 0 \
  >"$REPO_ROOT/.cache/media-mixer-test-rest-last.out" 2>&1
invalid_duration_status=$?
set -e
if [[ "$invalid_duration_status" -eq 0 ]]; then
  not_ok "start.sh rejects a non-positive guest broadcast duration"
else
  assert_contains "$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")" \
    "Invalid guest broadcast duration" \
    "start.sh reports an invalid guest broadcast duration"
fi

run_script stop
out="$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")"
assert_contains "$out" "stopped" "POST /stop returns stopped status"
assert_request "POST" "/stop" "stop.sh called POST /stop"

run_script switch_source publisher-a
out="$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")"
assert_contains "$out" "switched" "POST /switch returns switched status"
assert_request "POST" "/switch" "switch_source.sh called POST /switch"
body_line="$(grep '^BODY ' "$REQUEST_LOG" | tail -n1 | sed 's/^BODY //')"
if [[ "$(printf '%s' "$body_line" | jq -r '.participant_identity')" == "publisher-a" ]]; then
  ok "switch_source.sh sends the participant identity"
else
  not_ok "switch_source.sh sends the participant identity (body: $body_line)"
fi

set +e
"$MEDIA_MIXER_DIR/switch_source.sh" >"$REPO_ROOT/.cache/media-mixer-test-rest-last.out" 2>&1
switch_status=$?
set -e
if [[ "$switch_status" -eq 0 ]]; then
  not_ok "switch_source.sh without an identity should fail"
else
  assert_contains "$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")" \
    "PARTICIPANT_IDENTITY" "switch_source.sh reports the missing identity"
fi

run_script get_health
out="$(cat "$REPO_ROOT/.cache/media-mixer-test-rest-last.out")"
assert_contains "$out" '"status": "ok"' "get_health prints health status"
assert_contains "$out" '"state": "running"' "get_health prints mixer state"
assert_contains "$out" '"inputs": 0' "get_health prints input count"
assert_contains "$out" '"outputs": 1' "get_health prints output count"
assert_request "GET" "/health" "get_health called GET /health"

if printf '%s' "$out" | grep -qE '^\s{2}"status"'; then
  ok "get_health output is indented (human-readable)"
else
  not_ok "get_health output is indented (human-readable)"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
