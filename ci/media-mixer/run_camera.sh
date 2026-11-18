#!/usr/bin/env bash
#
# Create a virtual camera, stream a testing pattern through it, and remove the
# camera when the stream stops.
#
# Usage:
#   ./ci/media-mixer/run_camera.sh [-p|--pattern PATTERN] [timeout-seconds]
#
# The camera is configured with v4l2loopback and is detectable by browsers as a
# regular webcam. The stream stays active until the optional timeout expires or
# the script is interrupted with Ctrl+C. On exit the stream is stopped and the
# virtual camera is removed.
#
# Environment overrides:
#   MEDIA_MIXER_CAMERA_PATTERN          videotestsrc pattern
#   MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR   First /dev/videoN number (default: 10)
#   MEDIA_MIXER_CAMERA_LABEL_PREFIX     Browser-visible camera name prefix
#   MEDIA_MIXER_CAMERA_POOL_SIZE        Maximum parallel camera count (default: 8)
#   MEDIA_MIXER_CAMERA_STATE_DIR        Shared coordination directory
#
# When run on the host, the script re-executes itself in a privileged container
# with the host kernel modules and devices mounted, because it needs root
# privileges. Inside a container it runs directly as root.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_FIRST_VIDEO_NR=10
readonly DEFAULT_LABEL_PREFIX="CrowdStage Camera"
readonly DEFAULT_POOL_SIZE=8
readonly MAX_POOL_SIZE=8
readonly CONTAINER_STATE_DIR="/run/crowdstage-camera-runs"
readonly SYS_MODULE_DIR="${MEDIA_MIXER_CAMERA_SYS_MODULE_DIR:-/sys/module}"
readonly VIDEO_CLASS_DIR="${MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR:-/sys/class/video4linux}"
readonly DEVICE_DIR="${MEDIA_MIXER_CAMERA_DEVICE_DIR:-/dev}"

usage() {
  printf 'Usage: %s [-p|--pattern PATTERN] [timeout-seconds]\n' \
    "${BASH_SOURCE[0]##*/}" >&2
  printf 'Creates a virtual camera and streams until interrupted or the timeout expires.\n' >&2
}

run_in_container() {
  require_docker || exit 1

  local image
  image="$("$CI_DIR/ensure_builder.sh")" || exit 1

  local state_dir="${MEDIA_MIXER_CAMERA_STATE_DIR:-$CACHE_DIR/camera-runs}"
  mkdir -p "$state_dir"

  local -a env_args=(
    --env MEDIA_MIXER_CAMERA_IN_CONTAINER=1
    --env "MEDIA_MIXER_CAMERA_STATE_DIR=$CONTAINER_STATE_DIR"
  )
  local name
  for name in \
    MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR \
    MEDIA_MIXER_CAMERA_LABEL_PREFIX \
    MEDIA_MIXER_CAMERA_POOL_SIZE \
    MEDIA_MIXER_CAMERA_SYS_MODULE_DIR \
    MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR \
    MEDIA_MIXER_CAMERA_DEVICE_DIR; do
    if [[ -n "${!name+x}" ]]; then
      env_args+=(--env "$name")
    fi
  done

  exec docker run \
    --rm \
    --init \
    --privileged \
    --network none \
    --user 0 \
    --mount "type=bind,source=/lib/modules,target=/lib/modules,readonly" \
    --mount "type=bind,source=/dev,target=/dev" \
    --mount "type=bind,source=$REPO_ROOT,target=/src,readonly" \
    --mount "type=bind,source=$state_dir,target=$CONTAINER_STATE_DIR" \
    "${env_args[@]}" \
    --entrypoint bash \
    "$image" \
    "/src/ci/media-mixer/${BASH_SOURCE[0]##*/}" "$@"
}

pattern="${MEDIA_MIXER_CAMERA_PATTERN:-}"
timeout_seconds=""

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -p|--pattern)
      if [[ "$#" -lt 2 ]]; then
        err "Option $1 requires a videotestsrc pattern."
        usage
        exit 2
      fi
      if [[ -n "$pattern" ]]; then
        err "Video pattern was provided more than once: $pattern and $2"
        exit 2
      fi
      pattern="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      if [[ "$1" =~ ^[0-9]+$ ]]; then
        if [[ -n "$timeout_seconds" ]]; then
          err "Timeout was provided more than once: $1"
        else
          timeout_seconds="$1"
          shift
          continue
        fi
      elif [[ "$1" =~ ^-?[0-9]+$ ]]; then
        err "Timeout must be a non-negative integer number of seconds: $1"
      else
        err "Unknown argument: $1"
      fi
      usage
      exit 2
      ;;
  esac
done

if ! select_videotest_pattern pattern "$pattern"; then
  exit 2
fi

inside_container=0
if [[ -f /.dockerenv || "${MEDIA_MIXER_CAMERA_IN_CONTAINER:-0}" == "1" ]]; then
  inside_container=1
fi
if (( inside_container == 0 )); then
  container_args=(--pattern "$pattern")
  if [[ -n "$timeout_seconds" ]]; then
    container_args+=("$timeout_seconds")
  fi
  run_in_container "${container_args[@]}"
  exit $?
fi

if [[ "$(id -u)" != "0" ]]; then
  err "run_camera.sh must run as root inside the camera container."
  exit 1
fi

require_command flock
require_command gst-inspect-1.0
require_command gst-launch-1.0
require_command modprobe

if ! gst-inspect-1.0 v4l2sink >/dev/null 2>&1; then
  err "The GStreamer v4l2sink plugin is not available."
  exit 1
fi

first_video_nr="${MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR:-$DEFAULT_FIRST_VIDEO_NR}"
if [[ ! "$first_video_nr" =~ ^[0-9]+$ ]]; then
  err "MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR must be a non-negative integer: $first_video_nr"
  exit 2
fi
first_video_nr=$((10#$first_video_nr))

label_prefix="${MEDIA_MIXER_CAMERA_LABEL_PREFIX:-$DEFAULT_LABEL_PREFIX}"
if [[ -z "$label_prefix" || "$label_prefix" == *","* ]]; then
  err "MEDIA_MIXER_CAMERA_LABEL_PREFIX must be non-empty and must not contain a comma."
  exit 2
fi

pool_size="${MEDIA_MIXER_CAMERA_POOL_SIZE:-$DEFAULT_POOL_SIZE}"
if [[ ! "$pool_size" =~ ^[0-9]+$ ]]; then
  err "MEDIA_MIXER_CAMERA_POOL_SIZE must be a positive integer: $pool_size"
  exit 2
fi
pool_size=$((10#$pool_size))
if (( pool_size < 1 || pool_size > MAX_POOL_SIZE )); then
  err "MEDIA_MIXER_CAMERA_POOL_SIZE must be between 1 and $MAX_POOL_SIZE: $pool_size"
  exit 2
fi

state_dir="${MEDIA_MIXER_CAMERA_STATE_DIR:-$CACHE_DIR/camera-runs}"
if ! mkdir -p "$state_dir"; then
  err "Could not create the camera state directory: $state_dir"
  exit 1
fi
if [[ ! -w "$state_dir" ]]; then
  err "The camera state directory is not writable: $state_dir"
  exit 1
fi

pool_labels=()
pool_video_numbers=()
pool_devices=()
pool_exclusive_caps=()
for (( slot = 1; slot <= pool_size; slot++ )); do
  pool_labels+=("$label_prefix $slot")
  pool_video_numbers+=("$((first_video_nr + slot - 1))")
  pool_devices+=("$DEVICE_DIR/video$((first_video_nr + slot - 1))")
  pool_exclusive_caps+=(1)
done

label_list="$(IFS=,; printf '%s' "${pool_labels[*]}")"
video_nr_list="$(IFS=,; printf '%s' "${pool_video_numbers[*]}")"
exclusive_caps_list="$(IFS=,; printf '%s' "${pool_exclusive_caps[*]}")"

device_for_label() {
  local label="$1"
  local name_file
  local device_name

  for name_file in "$VIDEO_CLASS_DIR"/*/name; do
    [[ -e "$name_file" ]] || continue
    if [[ "$(cat "$name_file")" == "$label" ]]; then
      device_name="$(basename -- "$(dirname -- "$name_file")")"
      printf '%s/%s\n' "$DEVICE_DIR" "$device_name"
      return 0
    fi
  done
  return 1
}

module_matches_pool() {
  [[ -d "$SYS_MODULE_DIR/v4l2loopback" ]] || return 1

  local exclusive_caps_parameter="$SYS_MODULE_DIR/v4l2loopback/parameters/exclusive_caps"
  [[ -f "$exclusive_caps_parameter" ]] || return 1

  local -a current_exclusive_caps=()
  IFS=, read -r -a current_exclusive_caps <<<"$(cat "$exclusive_caps_parameter")"
  if (( ${#current_exclusive_caps[@]} < pool_size )); then
    return 1
  fi

  local slot
  for (( slot = 0; slot < pool_size; slot++ )); do
    case "${current_exclusive_caps[$slot]}" in
      Y|y|1) ;;
      *) return 1 ;;
    esac
  done

  local label device
  for label in "${pool_labels[@]}"; do
    if ! device="$(device_for_label "$label")" || [[ ! -e "$device" ]]; then
      return 1
    fi
  done
  return 0
}

claim_is_active() {
  local file="$1"
  [[ -e "$file" ]] || return 1
  if flock -n "$file" true 2>/dev/null; then
    return 1
  fi
  return 0
}

has_active_claims() {
  local file
  for file in "$state_dir"/slot-*.claim; do
    [[ -e "$file" ]] || continue
    if claim_is_active "$file"; then
      return 0
    fi
    rm -f "$file"
  done
  return 1
}

load_pool() {
  info "Configuring ${pool_size} virtual camera slot(s) for parallel runs." >&2
  if ! modprobe v4l2loopback \
    devices="$pool_size" \
    video_nr="$video_nr_list" \
    card_label="$label_list" \
    exclusive_caps="$exclusive_caps_list" \
    max_buffers=2; then
    err "Could not load the v4l2loopback kernel module."
    err "Install the module on the host (for example: sudo apt-get install v4l2loopback-dkms)."
    exit 1
  fi

  deadline=$((SECONDS + 5))
  local device
  for device in "${pool_devices[@]}"; do
    while [[ ! -e "$device" ]]; do
      if (( SECONDS >= deadline )); then
        err "Virtual camera device did not appear: $device"
        exit 1
      fi
      sleep 0.1
    done
  done
}

claim_file=""
media_pid=""
timer_pid=""
timeout_dir=""
timeout_marker=""

# shellcheck disable=SC2317 # invoked by the EXIT trap
cleanup() {
  if [[ -n "$timer_pid" ]]; then
    kill "$timer_pid" 2>/dev/null || true
    wait "$timer_pid" 2>/dev/null || true
    timer_pid=""
  fi
  if [[ -n "$media_pid" ]]; then
    stop_media_process "$media_pid"
    media_pid=""
  fi

  if [[ -n "$claim_file" ]]; then
    flock -x 9 2>/dev/null || true
    rm -f "$claim_file"
    exec 8>&-
    if ! has_active_claims; then
      modprobe -r v4l2loopback 2>/dev/null || true
    fi
    flock -u 9 2>/dev/null || true
    claim_file=""
  fi

  if [[ -n "$timeout_dir" ]]; then
    rm -rf "$timeout_dir"
    timeout_dir=""
  fi
}

exec 9>"$state_dir/state.lock"
flock -x 9

has_active_claims && active_claims=1 || active_claims=0

if ! module_matches_pool; then
  if (( active_claims == 1 )); then
    err "v4l2loopback is in use by another camera run with different settings."
    err "Stop the other run_camera.sh instances and try again."
    exit 1
  fi
  if [[ -d "$SYS_MODULE_DIR/v4l2loopback" ]]; then
    if ! modprobe -r v4l2loopback; then
      err "v4l2loopback is already loaded with different settings and cannot be unloaded."
      err "Stop the processes using it and try again."
      exit 1
    fi
  fi
  load_pool
fi

slot=""
for (( candidate = 1; candidate <= pool_size; candidate++ )); do
  candidate_claim="$state_dir/slot-$candidate.claim"
  if [[ ! -e "$candidate_claim" ]]; then
    slot=$candidate
    claim_file="$candidate_claim"
    break
  fi
done

if [[ -z "$slot" ]]; then
  err "All $pool_size virtual camera slots are in use."
  exit 1
fi

exec 8>"$claim_file"
flock -x 8
flock -u 9

camera_label="${pool_labels[$((slot - 1))]}"
camera_device="${pool_devices[$((slot - 1))]}"

trap cleanup EXIT
trap 'exit 0' INT TERM

if [[ -n "$timeout_seconds" ]]; then
  timeout_dir="$(mktemp -d "${TMPDIR:-/tmp}/crowdstage-camera-timeout.XXXXXX")"
  timeout_marker="$timeout_dir/timed-out"
fi

info "Streaming pattern ${pattern} through virtual camera ${camera_device} (${camera_label})"

gst_args=(
  videotestsrc is-live=true pattern="$pattern" '!'
  'video/x-raw,width=640,height=480,framerate=30/1' '!'
  videoconvert '!'
  'video/x-raw,format=YUY2' '!'
  v4l2sink device="$camera_device" sync=false
)

gst-launch-1.0 "${gst_args[@]}" &
media_pid=$!

if [[ -n "$timeout_seconds" ]]; then
  (
    sleep "$timeout_seconds"
    if kill -0 "$media_pid" 2>/dev/null; then
      : >"$timeout_marker"
      stop_media_process "$media_pid"
    fi
  ) &
  timer_pid=$!
fi

status=0
wait "$media_pid" || status=$?
media_pid=""

if [[ -n "$timer_pid" ]]; then
  kill "$timer_pid" 2>/dev/null || true
  wait "$timer_pid" 2>/dev/null || true
  timer_pid=""
fi

timed_out=0
if [[ -n "$timeout_marker" && -f "$timeout_marker" ]]; then
  timed_out=1
fi

if [[ "$timed_out" -eq 1 && "$status" -eq 0 ]]; then
  info "Camera stream on ${camera_device} stopped after ${timeout_seconds}s."
  exit 0
fi

exit "$status"
