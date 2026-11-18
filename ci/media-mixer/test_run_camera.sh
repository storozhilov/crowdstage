#!/usr/bin/env bash
#
# Tests for ci/media-mixer/run_camera.sh argument parsing and pipeline setup.
#
# The script is exercised with a fake docker command and a regular file standing
# in for a virtual camera device, so no kernel module or development stand is
# required.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

TEST_DIR="$(mktemp -d)"
readonly TEST_DIR
readonly FAKE_BIN="$TEST_DIR/bin"
readonly FAKE_DOCKER_LOG="$TEST_DIR/docker.log"
readonly SCRIPT_OUTPUT="$TEST_DIR/output"
readonly FAKE_DEVICE="$TEST_DIR/video10"

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
: >"$FAKE_DEVICE"

cat >"$FAKE_BIN/docker" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
if [[ "${1:-}" == "image" ]]; then
  exit 0
fi
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
    TMPDIR="$TEST_DIR" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    FAKE_DOCKER_RUN_EXIT="${FAKE_DOCKER_RUN_EXIT:-0}" \
    FAKE_DOCKER_RUN_SLEEP="${FAKE_DOCKER_RUN_SLEEP:-0}" \
    FAKE_DOCKER_SIGNAL_EXIT="${FAKE_DOCKER_SIGNAL_EXIT:-0}" \
    MEDIA_MIXER_CAMERA_IMAGE="test-camera-image" \
    bash "$MEDIA_MIXER_DIR/run_camera.sh" "$@" >"$SCRIPT_OUTPUT" 2>&1 || rc=$?
  return "$rc"
}

run_script --device "$FAKE_DEVICE" --pattern smpte75
docker_log="$(cat "$FAKE_DOCKER_LOG")"
assert_contains "$docker_log" "--device $FAKE_DEVICE" "the selected camera device is passed to Docker"
assert_contains "$docker_log" "--group-add" "the camera device group is granted to the container"
assert_contains "$docker_log" "test-camera-image" "the configured camera publisher image is used"
assert_contains "$docker_log" "pattern=smpte75" "the configured video pattern is used"
assert_contains "$docker_log" "v4l2sink device=$FAKE_DEVICE" \
  "the test pattern is written to the selected camera"
assert_contains "$docker_log" "format=YUY2" "the camera stream uses a webcam-compatible format"
assert_contains "$docker_log" "width=640,height=480" "the camera stream uses a standard resolution"

run_script -d "$FAKE_DEVICE" -p ball
assert_contains "$(cat "$FAKE_DOCKER_LOG")" "pattern=ball" "-d and -p select the device and pattern"

FAKE_DOCKER_RUN_SLEEP=10
run_script "$FAKE_DEVICE" 3
assert_contains "$(cat "$FAKE_DOCKER_LOG")" "v4l2sink device=$FAKE_DEVICE" \
  "a positional device is accepted"
assert_contains "$(cat "$SCRIPT_OUTPUT")" "stopped after 3s" \
  "the optional timeout stops the camera stream"

run_script --device "$FAKE_DEVICE" --pattern smpte75 3
assert_contains "$(cat "$SCRIPT_OUTPUT")" "stopped after 3s" \
  "the optional timeout works with explicit options"
FAKE_DOCKER_RUN_SLEEP=0

if run_script --device "$TEST_DIR/missing" --pattern smpte75; then
  not_ok "a missing camera device should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "does not exist" \
    "a missing camera device reports a clear error"
fi

if run_script --device; then
  not_ok "--device without a value should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "requires a device path" \
    "--device without a value reports a missing device"
fi

if run_script --device "$FAKE_DEVICE" --pattern not-a-pattern; then
  not_ok "an unsupported video pattern should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Unsupported videotestsrc pattern" \
    "an unsupported video pattern reports a clear error"
fi

if run_script --device "$FAKE_DEVICE" --unknown; then
  not_ok "an unknown argument should fail"
else
  assert_contains "$(cat "$SCRIPT_OUTPUT")" "Unknown argument" \
    "an unknown argument is rejected"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
