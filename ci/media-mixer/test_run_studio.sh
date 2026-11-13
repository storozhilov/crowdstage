#!/usr/bin/env bash
#
# Tests for ci/media-mixer/run_studio.sh argument parsing and pipeline setup.
#
# The script is exercised with a fake docker command so no development stand is
# required. The fake records the docker arguments, which lets the tests verify
# the LiveKit room, identities, media caps, and source pattern that would be
# published.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

TEST_DIR="$(mktemp -d)"
readonly TEST_DIR
readonly FAKE_BIN="$TEST_DIR/bin"
readonly FAKE_DOCKER_LOG="$TEST_DIR/docker.log"
readonly SCRIPT_OUTPUT="$TEST_DIR/output"
readonly TEST_TMPDIR="$TEST_DIR/tmp"
readonly TMPDIR_SNAPSHOT="$TEST_DIR/tmpdir-snapshot"

cleanup() {
  # shellcheck disable=SC2317 # invoked by the EXIT trap
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

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

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"

  if printf '%s' "$haystack" | grep -qF -- "$needle"; then
    not_ok "$label (output: $haystack)"
  else
    ok "$label"
  fi
}

mkdir -p "$FAKE_BIN" "$TEST_TMPDIR"
cat >"$FAKE_BIN/docker" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
if [[ "${1:-}" == "run" ]]; then
  if [[ -n "${FAKE_DOCKER_TMPDIR_SNAPSHOT:-}" ]]; then
    find "${TMPDIR:?}" -mindepth 1 -maxdepth 1 -printf '%f %m\n' >"$FAKE_DOCKER_TMPDIR_SNAPSHOT"
  fi
  trap 'exit "${FAKE_DOCKER_SIGNAL_EXIT:-0}"' INT TERM
  sleep_seconds="${FAKE_DOCKER_RUN_SLEEP:-0}"
  end=$((SECONDS + sleep_seconds))
  while (( SECONDS < end )); do
    sleep 0.1
  done
  exit "${FAKE_DOCKER_RUN_EXIT:-0}"
fi
exit 0
FAKE
chmod +x "$FAKE_BIN/docker"

run_script() {
  : >"$FAKE_DOCKER_LOG"
  local rc=0
  env -i \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    TMPDIR="$TEST_TMPDIR" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    FAKE_DOCKER_RUN_EXIT="${FAKE_DOCKER_RUN_EXIT:-0}" \
    FAKE_DOCKER_RUN_SLEEP="${FAKE_DOCKER_RUN_SLEEP:-0}" \
    FAKE_DOCKER_SIGNAL_EXIT="${FAKE_DOCKER_SIGNAL_EXIT:-0}" \
    FAKE_DOCKER_TMPDIR_SNAPSHOT="${FAKE_DOCKER_TMPDIR_SNAPSHOT:-}" \
    LIVEKIT_URL="ws://livekit:7880" \
    LIVEKIT_API_KEY="key" \
    LIVEKIT_API_SECRET="secret" \
    MEDIA_MIXER_SOURCE_ROOM="studio" \
    MEDIA_MIXER_STUDIO_IDENTITY="test-identity" \
    MEDIA_MIXER_STUDIO_PATTERN="smpte75" \
    bash "$MEDIA_MIXER_DIR/run_studio.sh" "$@" >"$SCRIPT_OUTPUT" 2>&1 || rc=$?
  return "$rc"
}

run_script
docker_log="$(cat "$FAKE_DOCKER_LOG")"
assert_contains "$docker_log" 'signaller::room-name=studio' \
  "the configured source room is used"
assert_contains "$docker_log" 'signaller::identity=test-identity-video' \
  "the video publisher identity is derived from the base identity"
assert_contains "$docker_log" 'signaller::identity=test-identity-audio' \
  "the audio publisher identity is derived from the base identity"
assert_contains "$docker_log" 'pattern=smpte75' \
  "the configured video pattern is used"
assert_contains "$docker_log" 'video-caps=video/x-vp8' \
  "the video publisher proposes VP8"
assert_contains "$docker_log" 'audio-caps=audio/x-opus' \
  "the audio publisher proposes Opus"

FAKE_DOCKER_RUN_SLEEP=10
FAKE_DOCKER_TMPDIR_SNAPSHOT="$TMPDIR_SNAPSHOT"
if run_script 3; then
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "stopped after 3s" \
    "the optional timeout stops the publisher"
else
  not_ok "an optional timeout should succeed"
fi
FAKE_DOCKER_RUN_SLEEP=0
FAKE_DOCKER_TMPDIR_SNAPSHOT=""

tmpdir_snapshot=""
if [[ -f "$TMPDIR_SNAPSHOT" ]]; then
  tmpdir_snapshot="$(cat "$TMPDIR_SNAPSHOT")"
fi
assert_contains "$tmpdir_snapshot" "crowdstage-studio-timeout." \
  "the timeout marker uses a private temporary directory"
if [[ "$tmpdir_snapshot" =~ crowdstage-studio-timeout\.[^[:space:]]+[[:space:]]700 ]]; then
  ok "the timeout marker directory is private"
else
  not_ok "the timeout marker directory should have mode 700 (snapshot: $tmpdir_snapshot)"
fi
if [[ -z "$(find "$TEST_TMPDIR" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
  ok "the timeout marker directory is removed after the run"
else
  not_ok "the timeout marker directory should be removed after the run"
fi

FAKE_DOCKER_RUN_EXIT=7
set +e
run_script 3
rc=$?
set -e
FAKE_DOCKER_RUN_EXIT=0
if [[ "$rc" -eq 7 ]]; then
  ok "a timed run preserves an early publisher failure"
else
  not_ok "a timed run should preserve an early publisher failure (exit $rc)"
fi
assert_not_contains "$(cat "$SCRIPT_OUTPUT")" "stopped after" \
  "a failed timed run is not reported as a timeout"

FAKE_DOCKER_RUN_SLEEP=10
FAKE_DOCKER_SIGNAL_EXIT=7
set +e
run_script 1
rc=$?
set -e
FAKE_DOCKER_RUN_SLEEP=0
FAKE_DOCKER_SIGNAL_EXIT=0
if [[ "$rc" -eq 7 ]]; then
  ok "a timeout does not mask a publisher error exit"
else
  not_ok "a timeout should not mask a publisher error exit (exit $rc)"
fi
assert_not_contains "$(cat "$SCRIPT_OUTPUT")" "stopped after" \
  "a publisher error at the timeout boundary is not reported as a timeout"

if run_script --unknown; then
  not_ok "an unknown argument should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Unknown argument" \
    "an unknown argument is rejected"
fi

if run_script 5x; then
  not_ok "a malformed timeout should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Timeout must be a non-negative integer" \
    "a malformed timeout is rejected"
fi

if run_script -1; then
  not_ok "a negative timeout should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Timeout must be a non-negative integer" \
    "a negative timeout is rejected"
fi

if run_script 1 2; then
  not_ok "a duplicate timeout should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Timeout was provided more than once" \
    "a duplicate timeout is rejected"
fi

set +e
env -i \
  PATH="$FAKE_BIN:$PATH" \
  HOME="$HOME" \
  FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
  LIVEKIT_URL="ws://livekit:7880" \
  LIVEKIT_API_KEY="key" \
  LIVEKIT_API_SECRET="secret" \
  MEDIA_MIXER_SOURCE_ROOM="" \
  bash "$MEDIA_MIXER_DIR/run_studio.sh" >"$SCRIPT_OUTPUT" 2>&1
rc=$?
set -e
if [[ "$rc" -eq 0 ]]; then
  not_ok "a missing studio room should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "MEDIA_MIXER_SOURCE_ROOM must be set" \
    "a missing studio room is reported"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
