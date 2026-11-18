#!/usr/bin/env bash
#
# Tests for ci/media-mixer/list_cameras.sh virtual camera discovery.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

TEST_DIR="$(mktemp -d)"
readonly TEST_DIR
readonly FAKE_VIDEO_CLASS_DIR="$TEST_DIR/sys/class/video4linux"
readonly FAKE_DEVICE_DIR="$TEST_DIR/dev"
readonly SCRIPT_OUTPUT="$TEST_DIR/output"
readonly SCRIPT_ERROR="$TEST_DIR/error"

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

mkdir -p "$FAKE_VIDEO_CLASS_DIR/video10" \
  "$FAKE_VIDEO_CLASS_DIR/video11" \
  "$FAKE_VIDEO_CLASS_DIR/video12" \
  "$FAKE_VIDEO_CLASS_DIR/video13" \
  "$FAKE_DEVICE_DIR"
printf 'CrowdStage Camera 1\n' >"$FAKE_VIDEO_CLASS_DIR/video10/name"
printf 'CrowdStage Camera 2\n' >"$FAKE_VIDEO_CLASS_DIR/video11/name"
printf 'Unrelated Loopback\n' >"$FAKE_VIDEO_CLASS_DIR/video12/name"
printf 'CrowdStage Camera 3\n' >"$FAKE_VIDEO_CLASS_DIR/video13/name"
: >"$FAKE_DEVICE_DIR/video10"
: >"$FAKE_DEVICE_DIR/video11"
: >"$FAKE_DEVICE_DIR/video12"

run_script() {
  local rc=0
  env -i \
    PATH="$PATH" \
    HOME="$HOME" \
    MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR="$FAKE_VIDEO_CLASS_DIR" \
    MEDIA_MIXER_CAMERA_DEVICE_DIR="$FAKE_DEVICE_DIR" \
    bash "$MEDIA_MIXER_DIR/list_cameras.sh" "$@" >"$SCRIPT_OUTPUT" 2>"$SCRIPT_ERROR" || rc=$?
  return "$rc"
}

run_script
out="$(cat "$SCRIPT_OUTPUT")"
assert_contains "$out" "$FAKE_DEVICE_DIR/video10	CrowdStage Camera 1" \
  "the first configured camera is listed with its label"
assert_contains "$out" "$FAKE_DEVICE_DIR/video11	CrowdStage Camera 2" \
  "the second configured camera is listed with its label"
assert_not_contains "$out" "Unrelated Loopback" "unrelated loopback devices are omitted"
assert_not_contains "$out" "video13" "devices without a device node are omitted"

empty_dir="$TEST_DIR/empty"
mkdir -p "$empty_dir"
env -i \
  PATH="$PATH" \
  HOME="$HOME" \
  MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR="$empty_dir" \
  MEDIA_MIXER_CAMERA_DEVICE_DIR="$FAKE_DEVICE_DIR" \
  bash "$MEDIA_MIXER_DIR/list_cameras.sh" >"$SCRIPT_OUTPUT" 2>"$SCRIPT_ERROR"
if [[ ! -s "$SCRIPT_OUTPUT" ]]; then
  ok "an empty camera directory produces no output"
else
  not_ok "an empty camera directory should produce no output"
fi

if run_script --help; then
  assert_contains "$(cat "$SCRIPT_ERROR")" "Lists configured virtual cameras" \
    "--help describes the listing"
else
  not_ok "--help should succeed"
fi

if run_script --unknown; then
  not_ok "an unknown argument should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "Unknown argument" \
    "an unknown argument is rejected"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
