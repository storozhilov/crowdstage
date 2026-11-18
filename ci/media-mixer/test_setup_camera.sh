#!/usr/bin/env bash
#
# Tests for ci/media-mixer/setup_camera.sh argument handling and module setup.
#
# The script is exercised with fake id and modprobe commands plus temporary
# sysfs and device directories, so no kernel module or root access is required.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

TEST_DIR="$(mktemp -d)"
readonly TEST_DIR
readonly FAKE_BIN="$TEST_DIR/bin"
readonly FAKE_MODPROBE_LOG="$TEST_DIR/modprobe.log"
readonly FAKE_SYS_MODULE_DIR="$TEST_DIR/sys/module"
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

mkdir -p "$FAKE_BIN" "$FAKE_SYS_MODULE_DIR" "$FAKE_VIDEO_CLASS_DIR" "$FAKE_DEVICE_DIR"

cat >"$FAKE_BIN/id" <<'FAKE'
#!/usr/bin/env bash
if [[ "${1:-}" == "-u" ]]; then
  printf '%s\n' "${FAKE_UID:-0}"
else
  printf '%s\n' "${FAKE_GID:-0}"
fi
FAKE
chmod +x "$FAKE_BIN/id"

cat >"$FAKE_BIN/modprobe" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_MODPROBE_LOG"
if [[ "${1:-}" == "-r" ]]; then
  exit 0
fi

first_video_nr=""
count=""
for arg in "$@"; do
  case "$arg" in
    devices=*) count="${arg#devices=}" ;;
    video_nr=*) first_video_nr="${arg#video_nr=}" ;;
  esac
done
first_video_nr="${first_video_nr%%,*}"
for (( index = 0; index < ${count:-0}; index++ )); do
  : >"${FAKE_DEVICE_DIR:?}/video$((first_video_nr + index))"
done
FAKE
chmod +x "$FAKE_BIN/modprobe"

run_script() {
  : >"$FAKE_MODPROBE_LOG"
  local rc=0
  env -i \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    FAKE_MODPROBE_LOG="$FAKE_MODPROBE_LOG" \
    FAKE_DEVICE_DIR="$FAKE_DEVICE_DIR" \
    FAKE_UID="${FAKE_UID:-0}" \
    FAKE_GID="${FAKE_GID:-0}" \
    MEDIA_MIXER_CAMERA_SYS_MODULE_DIR="$FAKE_SYS_MODULE_DIR" \
    MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR="$FAKE_VIDEO_CLASS_DIR" \
    MEDIA_MIXER_CAMERA_DEVICE_DIR="$FAKE_DEVICE_DIR" \
    bash "$MEDIA_MIXER_DIR/setup_camera.sh" "$@" >"$SCRIPT_OUTPUT" 2>"$SCRIPT_ERROR" || rc=$?
  return "$rc"
}

run_script 2
out="$(cat "$SCRIPT_OUTPUT")"
assert_contains "$out" "$FAKE_DEVICE_DIR/video10" "the first camera device path is printed"
assert_contains "$out" "$FAKE_DEVICE_DIR/video11" "the second camera device path is printed"
if [[ "$(printf '%s\n' "$out" | wc -l)" -eq 2 ]]; then
  ok "stdout contains only the device paths"
else
  not_ok "stdout should contain only the device paths (output: $out)"
fi
assert_contains "$(cat "$FAKE_MODPROBE_LOG")" "devices=2" "the requested camera count is passed"
assert_contains "$(cat "$FAKE_MODPROBE_LOG")" "video_nr=10,11" "consecutive video devices are requested"
assert_contains "$(cat "$FAKE_MODPROBE_LOG")" "card_label=CrowdStage Camera 1,CrowdStage Camera 2" \
  "browser-visible camera labels are configured"
assert_contains "$(cat "$FAKE_MODPROBE_LOG")" "exclusive_caps=1,1" \
  "exclusive capture capability is enabled for every camera"

mkdir -p "$FAKE_SYS_MODULE_DIR/v4l2loopback/parameters" \
  "$FAKE_VIDEO_CLASS_DIR/video10" "$FAKE_VIDEO_CLASS_DIR/video11"
printf 'Y,Y,N,N,N,N,N,N\n' >"$FAKE_SYS_MODULE_DIR/v4l2loopback/parameters/exclusive_caps"
printf 'CrowdStage Camera 1\n' >"$FAKE_VIDEO_CLASS_DIR/video10/name"
printf 'CrowdStage Camera 2\n' >"$FAKE_VIDEO_CLASS_DIR/video11/name"
: >"$FAKE_DEVICE_DIR/video10"
: >"$FAKE_DEVICE_DIR/video11"

run_script 2
out="$(cat "$SCRIPT_OUTPUT")"
assert_contains "$out" "$FAKE_DEVICE_DIR/video10" "existing matching cameras are reused"
assert_contains "$out" "$FAKE_DEVICE_DIR/video11" "existing matching cameras are reused"
if [[ -s "$FAKE_MODPROBE_LOG" ]]; then
  not_ok "matching existing cameras should not reload the module"
else
  ok "matching existing cameras should not reload the module"
fi

printf 'Y,N,N,N,N,N,N,N\n' >"$FAKE_SYS_MODULE_DIR/v4l2loopback/parameters/exclusive_caps"
run_script 2
if [[ -s "$FAKE_MODPROBE_LOG" ]]; then
  ok "cameras without exclusive capture capability are reconfigured"
else
  not_ok "cameras without exclusive capture capability should be reconfigured"
fi

rm -rf "$FAKE_SYS_MODULE_DIR/v4l2loopback"
FAKE_UID=1000
if run_script 1; then
  not_ok "a non-root invocation should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "must run as root" \
    "a non-root invocation reports the required privilege"
fi
FAKE_UID=0

if run_script 0; then
  not_ok "a zero camera count should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "positive integer" \
    "a zero camera count is rejected"
fi

if run_script many; then
  not_ok "a non-numeric camera count should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "positive integer" \
    "a non-numeric camera count is rejected"
fi

if run_script 1 2; then
  not_ok "a duplicate camera count should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "provided more than once" \
    "a duplicate camera count is rejected"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
