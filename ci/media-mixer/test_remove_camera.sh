#!/usr/bin/env bash
#
# Tests for ci/media-mixer/remove_camera.sh target selection and module reload.
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
readonly FAKE_DOCKER_LOG="$TEST_DIR/docker.log"
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

assert_empty() {
  local value="$1"
  local label="$2"

  if [[ -z "$value" ]]; then
    ok "$label"
  else
    not_ok "$label (output: $value)"
  fi
}

mkdir -p "$FAKE_BIN" "$FAKE_VIDEO_CLASS_DIR" "$FAKE_DEVICE_DIR"

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
  exit "${FAKE_MODPROBE_REMOVE_EXIT:-0}"
fi

first_video_nr=""
count=""
for arg in "$@"; do
  case "$arg" in
    devices=*) count="${arg#devices=}" ;;
    video_nr=*) first_video_nr="${arg#video_nr=}" ;;
  esac
done
IFS=, read -r -a video_numbers <<<"$first_video_nr"
for video_number in "${video_numbers[@]}"; do
  : >"${FAKE_DEVICE_DIR:?}/video$video_number"
done
FAKE
chmod +x "$FAKE_BIN/modprobe"

cat >"$FAKE_BIN/docker" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
exit 0
FAKE
chmod +x "$FAKE_BIN/docker"

reset_cameras() {
  rm -rf "$FAKE_VIDEO_CLASS_DIR" "$FAKE_DEVICE_DIR"
  mkdir -p "$FAKE_VIDEO_CLASS_DIR" "$FAKE_DEVICE_DIR"

  local video_number
  for video_number in "$@"; do
    mkdir -p "$FAKE_VIDEO_CLASS_DIR/video$video_number"
    printf 'CrowdStage Camera %s\n' "$((video_number - 9))" \
      >"$FAKE_VIDEO_CLASS_DIR/video$video_number/name"
    : >"$FAKE_DEVICE_DIR/video$video_number"
  done
}

run_script() {
  : >"$FAKE_MODPROBE_LOG"
  local rc=0
  env -i \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    FAKE_MODPROBE_LOG="$FAKE_MODPROBE_LOG" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    FAKE_DEVICE_DIR="$FAKE_DEVICE_DIR" \
    FAKE_UID="${FAKE_UID:-0}" \
    FAKE_GID="${FAKE_GID:-0}" \
    FAKE_MODPROBE_REMOVE_EXIT="${FAKE_MODPROBE_REMOVE_EXIT:-0}" \
    MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR="$FAKE_VIDEO_CLASS_DIR" \
    MEDIA_MIXER_CAMERA_DEVICE_DIR="$FAKE_DEVICE_DIR" \
    MEDIA_MIXER_CAMERA_IN_CONTAINER=1 \
    bash "$MEDIA_MIXER_DIR/remove_camera.sh" "$@" >"$SCRIPT_OUTPUT" 2>"$SCRIPT_ERROR" || rc=$?
  return "$rc"
}

run_host_script() {
  : >"$FAKE_DOCKER_LOG"
  local rc=0
  env -i \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    FAKE_UID=0 \
    FAKE_GID=0 \
    MEDIA_MIXER_CAMERA_LABEL_PREFIX="Test Camera" \
    bash "$MEDIA_MIXER_DIR/remove_camera.sh" "$@" >"$SCRIPT_OUTPUT" 2>"$SCRIPT_ERROR" || rc=$?
  return "$rc"
}

reset_cameras 10 11
run_host_script /dev/video11
docker_log="$(cat "$FAKE_DOCKER_LOG")"
assert_contains "$docker_log" "run --rm --privileged --network none --user 0" \
  "host execution re-runs removal in a privileged container"
assert_contains "$docker_log" "source=/lib/modules,target=/lib/modules,readonly" \
  "the host kernel modules are mounted"
assert_contains "$docker_log" "source=/dev,target=/dev" \
  "the host devices are mounted"
assert_contains "$docker_log" "source=$REPO_ROOT,target=/src,readonly" \
  "the repository is mounted"
assert_contains "$docker_log" "--env MEDIA_MIXER_CAMERA_LABEL_PREFIX" \
  "camera environment overrides are forwarded"
assert_contains "$docker_log" "/src/ci/media-mixer/remove_camera.sh /dev/video11" \
  "the script path and target are forwarded"

reset_cameras 10 11
run_script /dev/video11
log="$(cat "$FAKE_MODPROBE_LOG")"
assert_contains "$(cat "$SCRIPT_OUTPUT")" "$FAKE_DEVICE_DIR/video11" \
  "the removed device path is printed"
assert_contains "$log" "-r v4l2loopback" "the module is unloaded before reconfiguration"
assert_contains "$log" "devices=1" "the remaining camera count is passed"
assert_contains "$log" "video_nr=10" "remaining device numbers are preserved"
assert_contains "$log" "card_label=CrowdStage Camera 1" "remaining labels are preserved"
assert_contains "$log" "exclusive_caps=1" "remaining cameras keep exclusive capture"

reset_cameras 10 11
run_script 2
assert_contains "$(cat "$SCRIPT_OUTPUT")" "$FAKE_DEVICE_DIR/video11" \
  "a camera can be selected by its 1-based index"
assert_contains "$(cat "$FAKE_MODPROBE_LOG")" "video_nr=10" \
  "index selection removes the requested camera"

reset_cameras 10 11
run_script 'CrowdStage Camera 1'
assert_contains "$(cat "$SCRIPT_OUTPUT")" "$FAKE_DEVICE_DIR/video10" \
  "a camera can be selected by its label"
assert_contains "$(cat "$FAKE_MODPROBE_LOG")" "video_nr=11" \
  "label selection removes the requested camera"

reset_cameras 10
run_script video10
log="$(cat "$FAKE_MODPROBE_LOG")"
assert_contains "$(cat "$SCRIPT_OUTPUT")" "$FAKE_DEVICE_DIR/video10" \
  "a bare video device name is accepted"
assert_contains "$log" "-r v4l2loopback" "removing the last camera unloads the module"
assert_contains "$(cat "$SCRIPT_ERROR")" "no virtual cameras remain" \
  "removing the last camera is reported"
if grep -qE '^devices=' "$FAKE_MODPROBE_LOG"; then
  not_ok "removing the last camera should not reload the module"
else
  ok "removing the last camera should not reload the module"
fi

reset_cameras 10 11
if run_script /dev/video99; then
  not_ok "an unknown camera should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "Virtual camera not found" \
    "an unknown camera reports a clear error"
  assert_empty "$(cat "$FAKE_MODPROBE_LOG")" "an unknown camera does not reload the module"
fi

if run_script 3; then
  not_ok "an out-of-range camera index should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "out of range" \
    "an out-of-range camera index is rejected"
fi

FAKE_MODPROBE_REMOVE_EXIT=1
if run_script 1; then
  not_ok "a module unload failure should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "Could not unload v4l2loopback" \
    "a module unload failure reports an actionable error"
fi
FAKE_MODPROBE_REMOVE_EXIT=0

FAKE_UID=1000
if run_script 1; then
  not_ok "a non-root invocation should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "must run as root" \
    "a non-root invocation reports the required privilege"
fi
FAKE_UID=0

if run_script; then
  not_ok "a missing target should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "camera target is required" \
    "a missing target is rejected"
fi

if run_script 1 2; then
  not_ok "a duplicate target should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "provided more than once" \
    "a duplicate target is rejected"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
