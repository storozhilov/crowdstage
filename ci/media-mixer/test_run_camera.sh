#!/usr/bin/env bash
#
# Tests for ci/media-mixer/run_camera.sh virtual camera lifecycle and streaming.
#
# The scripts are exercised with fake id, modprobe, and GStreamer commands plus
# temporary sysfs, device, and state directories, so no kernel module, root
# access, or development stand is required.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

TEST_DIR="$(mktemp -d)"
readonly TEST_DIR
readonly FAKE_BIN="$TEST_DIR/bin"
readonly FAKE_MODPROBE_LOG="$TEST_DIR/modprobe.log"
readonly FAKE_GST_LOG="$TEST_DIR/gst.log"
readonly FAKE_DOCKER_LOG="$TEST_DIR/docker.log"
readonly FAKE_SYS_MODULE_DIR="$TEST_DIR/sys/module"
readonly FAKE_VIDEO_CLASS_DIR="$TEST_DIR/sys/class/video4linux"
readonly FAKE_DEVICE_DIR="$TEST_DIR/dev"
readonly FAKE_STATE_DIR="$TEST_DIR/state"
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

mkdir -p "$FAKE_BIN" "$FAKE_SYS_MODULE_DIR" "$FAKE_VIDEO_CLASS_DIR" \
  "$FAKE_DEVICE_DIR" "$FAKE_STATE_DIR"

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
  rm -rf "${FAKE_SYS_MODULE_DIR:?}/v4l2loopback"
  rm -rf "${FAKE_VIDEO_CLASS_DIR:?}"/video*
  rm -f "${FAKE_DEVICE_DIR:?}"/video*
  exit 0
fi

devices=""
video_nr=""
card_label=""
exclusive_caps=""
for arg in "$@"; do
  case "$arg" in
    devices=*) devices="${arg#devices=}" ;;
    video_nr=*) video_nr="${arg#video_nr=}" ;;
    card_label=*) card_label="${arg#card_label=}" ;;
    exclusive_caps=*) exclusive_caps="${arg#exclusive_caps=}" ;;
  esac
done

IFS=, read -r -a video_numbers <<<"$video_nr"
IFS=, read -r -a labels <<<"$card_label"
IFS=, read -r -a caps <<<"$exclusive_caps"

mkdir -p "$FAKE_SYS_MODULE_DIR/v4l2loopback/parameters"
printf '%s\n' "$exclusive_caps" >"$FAKE_SYS_MODULE_DIR/v4l2loopback/parameters/exclusive_caps"
mkdir -p "$FAKE_VIDEO_CLASS_DIR"
for (( index = 0; index < ${devices:-0}; index++ )); do
  name="${labels[$index]:-Camera $((index + 1))}"
  number="${video_numbers[$index]}"
  mkdir -p "$FAKE_VIDEO_CLASS_DIR/video$number"
  printf '%s\n' "$name" >"$FAKE_VIDEO_CLASS_DIR/video$number/name"
  : >"$FAKE_DEVICE_DIR/video$number"
done
FAKE
chmod +x "$FAKE_BIN/modprobe"

cat >"$FAKE_BIN/gst-inspect-1.0" <<'FAKE'
#!/usr/bin/env bash
exit 0
FAKE
chmod +x "$FAKE_BIN/gst-inspect-1.0"

cat >"$FAKE_BIN/gst-launch-1.0" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_GST_LOG"
trap 'exit 0' INT TERM
end=$((SECONDS + ${FAKE_GST_SLEEP:-0}))
while (( SECONDS < end )); do
  sleep 0.1
done
exit "${FAKE_GST_EXIT:-0}"
FAKE
chmod +x "$FAKE_BIN/gst-launch-1.0"

cat >"$FAKE_BIN/docker" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
exit 0
FAKE
chmod +x "$FAKE_BIN/docker"

reset_fixture() {
  rm -rf "$FAKE_SYS_MODULE_DIR" "$FAKE_VIDEO_CLASS_DIR" "$FAKE_DEVICE_DIR" \
    "$FAKE_STATE_DIR"
  mkdir -p "$FAKE_SYS_MODULE_DIR" "$FAKE_VIDEO_CLASS_DIR" \
    "$FAKE_DEVICE_DIR" "$FAKE_STATE_DIR"
  : >"$FAKE_MODPROBE_LOG"
  : >"$FAKE_GST_LOG"
  : >"$FAKE_DOCKER_LOG"
  : >"$SCRIPT_OUTPUT"
  : >"$SCRIPT_ERROR"
}

inner_env() {
  env -i \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    TMPDIR="$TEST_DIR" \
    FAKE_MODPROBE_LOG="$FAKE_MODPROBE_LOG" \
    FAKE_GST_LOG="$FAKE_GST_LOG" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    FAKE_SYS_MODULE_DIR="$FAKE_SYS_MODULE_DIR" \
    FAKE_VIDEO_CLASS_DIR="$FAKE_VIDEO_CLASS_DIR" \
    FAKE_DEVICE_DIR="$FAKE_DEVICE_DIR" \
    FAKE_UID="${FAKE_UID:-0}" \
    FAKE_GID="${FAKE_GID:-0}" \
    FAKE_GST_SLEEP="${FAKE_GST_SLEEP:-10}" \
    FAKE_GST_EXIT="${FAKE_GST_EXIT:-0}" \
    MEDIA_MIXER_CAMERA_IN_CONTAINER=1 \
    MEDIA_MIXER_CAMERA_POOL_SIZE="${MEDIA_MIXER_CAMERA_POOL_SIZE:-2}" \
    MEDIA_MIXER_CAMERA_STATE_DIR="$FAKE_STATE_DIR" \
    MEDIA_MIXER_CAMERA_SYS_MODULE_DIR="$FAKE_SYS_MODULE_DIR" \
    MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR="$FAKE_VIDEO_CLASS_DIR" \
    MEDIA_MIXER_CAMERA_DEVICE_DIR="$FAKE_DEVICE_DIR" \
    "$@"
}

run_inner() {
  local rc=0
  inner_env bash "$MEDIA_MIXER_DIR/run_camera.sh" "$@" \
    >"$SCRIPT_OUTPUT" 2>"$SCRIPT_ERROR" || rc=$?
  return "$rc"
}

run_host() {
  local rc=0
  env -i \
    PATH="$FAKE_BIN:$PATH" \
    HOME="$HOME" \
    FAKE_DOCKER_LOG="$FAKE_DOCKER_LOG" \
    FAKE_UID=0 \
    FAKE_GID=0 \
    MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR=20 \
    MEDIA_MIXER_CAMERA_STATE_DIR="$TEST_DIR/host-state" \
    bash "$MEDIA_MIXER_DIR/run_camera.sh" "$@" >"$SCRIPT_OUTPUT" 2>"$SCRIPT_ERROR" || rc=$?
  return "$rc"
}

reset_fixture
run_inner --pattern smpte75 3
modprobe_log="$(cat "$FAKE_MODPROBE_LOG")"
gst_log="$(cat "$FAKE_GST_LOG")"
assert_contains "$modprobe_log" "devices=2" "the parallel camera pool is configured"
assert_contains "$modprobe_log" "video_nr=10,11" "consecutive camera devices are requested"
assert_contains "$modprobe_log" "card_label=CrowdStage Camera 1,CrowdStage Camera 2" \
  "browser-visible camera labels are configured"
assert_contains "$modprobe_log" "exclusive_caps=1,1" \
  "exclusive capture capability is enabled for every slot"
assert_contains "$gst_log" "pattern=smpte75" "the configured video pattern is used"
assert_contains "$gst_log" "v4l2sink device=$FAKE_DEVICE_DIR/video10" \
  "the pattern is streamed through the created camera"
assert_contains "$gst_log" "format=YUY2" "the stream uses a webcam-compatible format"
assert_contains "$(cat "$SCRIPT_OUTPUT")" "stopped after 3s" \
  "the optional timeout stops the camera stream"
assert_contains "$modprobe_log" "-r v4l2loopback" \
  "the last run removes the virtual camera module"
if [[ ! -e "$FAKE_DEVICE_DIR/video10" ]]; then
  ok "the created camera device is removed on exit"
else
  not_ok "the created camera device should be removed on exit"
fi

reset_fixture
inner_env bash "$MEDIA_MIXER_DIR/run_camera.sh" --pattern smpte 3 \
  >"$TEST_DIR/first.out" 2>"$TEST_DIR/first.err" &
first_pid=$!
inner_env bash "$MEDIA_MIXER_DIR/run_camera.sh" --pattern ball 3 \
  >"$TEST_DIR/second.out" 2>"$TEST_DIR/second.err" &
second_pid=$!
wait "$first_pid"
wait "$second_pid"
modprobe_log="$(cat "$FAKE_MODPROBE_LOG")"
gst_log="$(cat "$FAKE_GST_LOG")"
assert_contains "$gst_log" "v4l2sink device=$FAKE_DEVICE_DIR/video10" \
  "the first parallel run uses its own camera"
assert_contains "$gst_log" "v4l2sink device=$FAKE_DEVICE_DIR/video11" \
  "the second parallel run uses another camera"
load_count="$(grep -c 'v4l2loopback devices=' "$FAKE_MODPROBE_LOG" || true)"
unload_count="$(grep -c '^-r v4l2loopback' "$FAKE_MODPROBE_LOG" || true)"
if [[ "$load_count" -eq 1 ]]; then
  ok "parallel runs share one camera pool"
else
  not_ok "parallel runs should share one camera pool (loads: $load_count)"
fi
if [[ "$unload_count" -eq 1 ]]; then
  ok "the camera pool is removed after the last parallel run"
else
  not_ok "the camera pool should be removed once after the last parallel run (unloads: $unload_count)"
fi

reset_fixture
if run_inner --pattern not-a-pattern; then
  not_ok "an unsupported video pattern should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "Unsupported videotestsrc pattern" \
    "an unsupported video pattern reports a clear error"
  if [[ ! -s "$FAKE_MODPROBE_LOG" ]]; then
    ok "an unsupported video pattern fails before configuring a camera"
  else
    not_ok "an unsupported video pattern should not configure a camera"
  fi
fi

if run_inner --pattern smpte --pattern ball; then
  not_ok "a duplicate video pattern should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "provided more than once" \
    "a duplicate video pattern is rejected"
fi

if run_inner --pattern smpte 1 2; then
  not_ok "a duplicate timeout should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "provided more than once" \
    "a duplicate timeout is rejected"
fi

if run_inner --unknown; then
  not_ok "an unknown argument should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "Unknown argument" \
    "an unknown argument is rejected"
fi

reset_fixture
FAKE_UID=1000
if run_inner --pattern smpte 1; then
  not_ok "a non-root invocation should fail"
else
  assert_contains "$(cat "$SCRIPT_ERROR")" "must run as root" \
    "a non-root invocation reports the required privilege"
fi
FAKE_UID=0

reset_fixture
run_host --pattern smpte75 5
docker_log="$(cat "$FAKE_DOCKER_LOG")"
assert_contains "$docker_log" "run --rm --init --privileged --network none --user 0" \
  "host execution runs the camera in a privileged container"
assert_contains "$docker_log" "source=/lib/modules,target=/lib/modules,readonly" \
  "the host kernel modules are mounted"
assert_contains "$docker_log" "source=/dev,target=/dev" \
  "the host devices are mounted"
assert_contains "$docker_log" "source=$REPO_ROOT,target=/src,readonly" \
  "the repository is mounted"
assert_contains "$docker_log" "target=/run/crowdstage-camera-runs" \
  "the camera coordination directory is mounted"
assert_contains "$docker_log" "--env MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR" \
  "camera environment overrides are forwarded"
assert_contains "$docker_log" "/src/ci/media-mixer/run_camera.sh --pattern smpte75 5" \
  "the script path, pattern, and timeout are forwarded"

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
