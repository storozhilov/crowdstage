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
readonly SEED_ENV="$TEST_DIR/bash_env"

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

: >"$SEED_ENV"

# Seeding Bash's RANDOM through BASH_ENV makes the random pattern selection
# reproducible without adding a test-only option to the script.
EXTRA_ENV=()

run_script() {
  : >"$FAKE_DOCKER_LOG"
  set +e
  env -i \
    BASH_ENV="$SEED_ENV" \
    "${EXTRA_ENV[@]}" \
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

participant_pattern() {
  grep -o 'pattern=[^ ]*' "$FAKE_DOCKER_LOG" | head -n1 | cut -d= -f2
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

EXTRA_ENV=("MOSAIC_PARTICIPANT_PATTERN=ball")
run_script
EXTRA_ENV=()
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'pattern=ball' \
  "MOSAIC_PARTICIPANT_PATTERN overrides the generated video pattern"

EXTRA_ENV=("MOSAIC_PARTICIPANT_PATTERN=solid-color")
run_script
EXTRA_ENV=()
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'pattern=solid-color' \
  "supported patterns outside the random set are accepted"

EXTRA_ENV=("MOSAIC_PARTICIPANT_PATTERN=not-a-pattern")
if run_script; then
  EXTRA_ENV=()
  not_ok "an unsupported video pattern should fail"
else
  EXTRA_ENV=()
  assert_contains "$(cat "$SCRIPT_OUTPUT")" \
    "Unsupported videotestsrc pattern: not-a-pattern" \
    "an unsupported video pattern reports a clear error"
  if [[ -s "$FAKE_DOCKER_LOG" ]]; then
    not_ok "an unsupported video pattern should not launch GStreamer"
  else
    ok "an unsupported video pattern fails before GStreamer starts"
  fi
fi

printf 'RANDOM=7\n' >"$SEED_ENV"
run_script
first_pattern="$(participant_pattern)"
run_script
second_pattern="$(participant_pattern)"
if [[ -n "$first_pattern" && "$first_pattern" == "$second_pattern" ]]; then
  ok "the same random seed selects the same video pattern"
else
  not_ok "the same random seed should select the same video pattern ('$first_pattern' vs '$second_pattern')"
fi

patterns=()
for seed in 1 2 3 4 5 6 7 8; do
  printf 'RANDOM=%s\n' "$seed" >"$SEED_ENV"
  run_script
  patterns+=("$(participant_pattern)")
done
: >"$SEED_ENV"
distinct_patterns="$(printf '%s\n' "${patterns[@]}" | sort -u | wc -l)"
if (( distinct_patterns > 1 )); then
  ok "different random seeds select different video patterns"
else
  not_ok "different random seeds should select different video patterns (got: ${patterns[*]})"
fi

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
