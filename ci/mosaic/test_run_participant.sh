#!/usr/bin/env bash
#
# Tests for ci/mosaic/run_participant.sh argument parsing.
#
# The script is exercised with a fake docker command so no development stand is
# required. The fake records the docker arguments, which lets the tests verify
# the LiveKit participant name that would be published.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

TEST_DIR="$(mktemp -d)"
readonly TEST_DIR
readonly FAKE_BIN="$TEST_DIR/bin"
readonly FAKE_DOCKER_LOG="$TEST_DIR/docker.log"
readonly SCRIPT_OUTPUT="$TEST_DIR/output"

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

mkdir -p "$FAKE_BIN"
cat >"$FAKE_BIN/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
exit 0
EOF
chmod +x "$FAKE_BIN/docker"

run_script() {
  : >"$FAKE_DOCKER_LOG"
  set +e
  env -i \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    LIVEKIT_URL="ws://livekit:7880" \
    LIVEKIT_API_KEY="key" \
    LIVEKIT_API_SECRET="secret" \
    MOSAIC_ROOM="participants" \
    MOSAIC_PARTICIPANT_IDENTITY="test-identity" \
    bash "$MOSAIC_DIR/run_participant.sh" "$@" >"$SCRIPT_OUTPUT" 2>&1
  local rc=$?
  set -e
  return "$rc"
}

run_script --name 'Vasya Pupkin'
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'signaller::participant-name=Vasya Pupkin' \
  "--name publishes the provided participant name"
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'signaller::identity=test-identity' \
  "--name keeps the participant identity"

run_script -n 'Vasya Pupkin'
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'signaller::participant-name=Vasya Pupkin' \
  "-n publishes the provided participant name"

run_script
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'signaller::participant-name=test-identity' \
  "the participant identity is used when no name is provided"

if run_script --name; then
  not_ok "--name without a value should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "requires a name" \
    "--name without a value reports a missing name"
fi

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

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
