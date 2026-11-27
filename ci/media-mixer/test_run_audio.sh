#!/usr/bin/env bash
#
# Tests for ci/media-mixer/run_audio.sh argument parsing and pipeline setup.
#
# The script is exercised with a fake docker command so no development stand is
# required. The fake records the docker arguments, which lets the tests verify
# the LiveKit room, identity, audio wave, frequency, and caps that would be
# published.

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
cat >"$FAKE_BIN/docker" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
if [[ "${1:-}" == "run" ]]; then
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

: >"$SEED_ENV"
EXTRA_ENV=()

run_script() {
  : >"$FAKE_DOCKER_LOG"
  local rc=0
  env -i \
    BASH_ENV="$SEED_ENV" \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    TMPDIR="$TEST_DIR" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    FAKE_DOCKER_RUN_EXIT="${FAKE_DOCKER_RUN_EXIT:-0}" \
    FAKE_DOCKER_RUN_SLEEP="${FAKE_DOCKER_RUN_SLEEP:-0}" \
    FAKE_DOCKER_SIGNAL_EXIT="${FAKE_DOCKER_SIGNAL_EXIT:-0}" \
    LIVEKIT_URL="ws://livekit:7880" \
    LIVEKIT_API_KEY="key" \
    LIVEKIT_API_SECRET="secret" \
    MEDIA_MIXER_SOURCE_ROOM="studio" \
    MEDIA_MIXER_AUDIO_IDENTITY="test-audio" \
    "${EXTRA_ENV[@]}" \
    bash "$MEDIA_MIXER_DIR/run_audio.sh" "$@" >"$SCRIPT_OUTPUT" 2>&1 || rc=$?
  return "$rc"
}

audio_wave() {
  grep -o 'wave=[^ ]*' "$FAKE_DOCKER_LOG" | head -n1 | cut -d= -f2
}

EXTRA_ENV=("MEDIA_MIXER_AUDIO_WAVE=sine" "MEDIA_MIXER_AUDIO_FREQUENCY=440")
run_script
docker_log="$(cat "$FAKE_DOCKER_LOG")"
assert_contains "$docker_log" 'signaller::room-name=studio' \
  "the configured source room is used"
assert_contains "$docker_log" 'signaller::identity=test-audio' \
  "the configured audio identity is used"
assert_contains "$docker_log" 'signaller::participant-name=test-audio' \
  "the participant name defaults to the audio identity"
assert_contains "$docker_log" 'audiotestsrc is-live=true wave=sine' \
  "the configured audio wave is used"
assert_contains "$docker_log" 'freq=440' \
  "the configured audio frequency is used"
assert_contains "$docker_log" 'audio-caps=audio/x-opus' \
  "the audio publisher proposes Opus"
assert_contains "$docker_log" 'audio/x-raw,rate=48000,channels=1' \
  "the audio publisher uses a standard raw audio format"
EXTRA_ENV=()

EXTRA_ENV=("MEDIA_MIXER_AUDIO_WAVE=triangle" "MEDIA_MIXER_AUDIO_FREQUENCY=660" "MEDIA_MIXER_AUDIO_VOLUME=0.3")
run_script --name 'Audio Test'
EXTRA_ENV=()
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'signaller::participant-name=Audio Test' \
  "--name publishes the provided participant name"
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'wave=triangle' \
  "MEDIA_MIXER_AUDIO_WAVE overrides the generated wave"
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'freq=660' \
  "MEDIA_MIXER_AUDIO_FREQUENCY overrides the generated frequency"
assert_contains "$(cat "$FAKE_DOCKER_LOG")" 'volume=0.3' \
  "MEDIA_MIXER_AUDIO_VOLUME overrides the test signal volume"

EXTRA_ENV=("MEDIA_MIXER_AUDIO_WAVE=not-a-wave")
if run_script; then
  EXTRA_ENV=()
  not_ok "an unsupported audio wave should fail"
else
  EXTRA_ENV=()
  assert_contains "$(cat "$SCRIPT_OUTPUT")" \
    "Unsupported audiotestsrc wave: not-a-wave" \
    "an unsupported audio wave reports a clear error"
  if [[ -s "$FAKE_DOCKER_LOG" ]]; then
    not_ok "an unsupported audio wave should not launch GStreamer"
  else
    ok "an unsupported audio wave fails before GStreamer starts"
  fi
fi

EXTRA_ENV=("MEDIA_MIXER_AUDIO_FREQUENCY=fast")
if run_script; then
  EXTRA_ENV=()
  not_ok "an invalid frequency should fail"
else
  EXTRA_ENV=()
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Invalid audio frequency" \
    "an invalid frequency reports a clear error"
fi

EXTRA_ENV=("MEDIA_MIXER_AUDIO_VOLUME=loud")
if run_script; then
  EXTRA_ENV=()
  not_ok "an invalid volume should fail"
else
  EXTRA_ENV=()
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Invalid audio volume" \
    "an invalid volume reports a clear error"
fi

printf 'RANDOM=7\n' >"$SEED_ENV"
run_script
first_wave="$(audio_wave)"
run_script
second_wave="$(audio_wave)"
if [[ -n "$first_wave" && "$first_wave" == "$second_wave" ]]; then
  ok "the same random seed selects the same audio wave"
else
  not_ok "the same random seed should select the same audio wave ('$first_wave' vs '$second_wave')"
fi

waves=()
for seed in 1 2 3 4 5 6 7 8; do
  printf 'RANDOM=%s\n' "$seed" >"$SEED_ENV"
  run_script
  waves+=("$(audio_wave)")
done
: >"$SEED_ENV"
distinct_waves="$(printf '%s\n' "${waves[@]}" | sort -u | wc -l)"
if (( distinct_waves > 1 )); then
  ok "different random seeds select different audio waves"
else
  not_ok "different random seeds should select different audio waves (got: ${waves[*]})"
fi

FAKE_DOCKER_RUN_SLEEP=10
if run_script 3; then
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "stopped after 3s" \
    "the optional timeout stops the publisher"
else
  not_ok "an optional timeout should succeed"
fi
FAKE_DOCKER_RUN_SLEEP=0

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

if run_script -1; then
  not_ok "a negative timeout should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Timeout must be a non-negative integer" \
    "a negative timeout is rejected"
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
  bash "$MEDIA_MIXER_DIR/run_audio.sh" >"$SCRIPT_OUTPUT" 2>&1
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
