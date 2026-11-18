#!/usr/bin/env bash
#
# Stream a testing pattern through a virtual camera configured by
# ci/media-mixer/setup_camera.sh. Browsers can then capture that camera as a
# regular webcam.
#
# Usage:
#   ./ci/media-mixer/run_camera.sh [-d|--device DEVICE] [-p|--pattern PATTERN] [timeout-seconds]
#
# The device defaults to MEDIA_MIXER_CAMERA_DEVICE or /dev/video0. The pattern
# defaults to MEDIA_MIXER_CAMERA_PATTERN or a random moving test pattern. The
# camera stream stays active until the optional timeout expires or the script
# is interrupted with Ctrl+C.
#
# Environment overrides:
#   MEDIA_MIXER_CAMERA_DEVICE  Virtual camera device path
#   MEDIA_MIXER_CAMERA_PATTERN videotestsrc pattern
#   MEDIA_MIXER_CAMERA_IMAGE   Runtime image with v4l2sink
#
# When executed inside a Docker container, the media pipeline runs directly.
# Outside Docker, it runs in the pinned runtime image.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_CAMERA_IMAGE="livekit/gstreamer:1.26.11-prod-rs"

usage() {
  printf 'Usage: %s [-d|--device DEVICE] [-p|--pattern PATTERN] [timeout-seconds]\n' \
    "${BASH_SOURCE[0]##*/}" >&2
  printf 'Runs until interrupted when no timeout is provided.\n' >&2
}

device="${MEDIA_MIXER_CAMERA_DEVICE:-/dev/video0}"
device_set=0
pattern="${MEDIA_MIXER_CAMERA_PATTERN:-}"
timeout_seconds=""

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -d|--device)
      if [[ "$#" -lt 2 ]]; then
        err "Option $1 requires a device path."
        usage
        exit 2
      fi
      if (( device_set == 1 )); then
        err "Camera device was provided more than once: $device and $2"
        exit 2
      fi
      device="$2"
      device_set=1
      shift 2
      ;;
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
      elif [[ "$1" != -* ]]; then
        if (( device_set == 1 )); then
          err "Camera device was provided more than once: $device and $1"
          exit 2
        fi
        device="$1"
        device_set=1
        shift
        continue
      else
        err "Unknown argument: $1"
      fi
      usage
      exit 2
      ;;
  esac
done

if [[ ! -e "$device" ]]; then
  err "Virtual camera device does not exist: $device"
  err "Run ci/media-mixer/setup_camera.sh first and pass one of the printed devices."
  exit 1
fi

if ! select_videotest_pattern pattern "$pattern"; then
  exit 2
fi

inside_container=0
if [[ -f /.dockerenv ]]; then
  inside_container=1
fi

container_name=""
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
  if [[ -n "$container_name" ]]; then
    docker rm --force "$container_name" >/dev/null 2>&1 || true
  fi
  if [[ -n "$timeout_dir" ]]; then
    rm -rf "$timeout_dir"
    timeout_dir=""
  fi
}

trap cleanup EXIT
trap 'exit 0' INT TERM

if [[ -n "$timeout_seconds" ]]; then
  timeout_dir="$(mktemp -d "${TMPDIR:-/tmp}/crowdstage-camera-timeout.XXXXXX")"
  timeout_marker="$timeout_dir/timed-out"
fi

info "Streaming pattern ${pattern} to virtual camera ${device}"
info "The camera stream will remain active until stopped."

gst_args=(
  videotestsrc is-live=true pattern="$pattern" '!'
  'video/x-raw,width=640,height=480,framerate=30/1' '!'
  videoconvert '!'
  'video/x-raw,format=YUY2' '!'
  v4l2sink device="$device" sync=false
)

if [[ "$inside_container" -eq 1 ]]; then
  require_command gst-launch-1.0
  require_command gst-inspect-1.0
  if ! gst-inspect-1.0 v4l2sink >/dev/null 2>&1; then
    err "The GStreamer v4l2sink plugin is not available."
    exit 1
  fi
  media_command=(gst-launch-1.0 "${gst_args[@]}")
else
  require_docker || exit 1
  camera_image="${MEDIA_MIXER_CAMERA_IMAGE:-${MEDIA_MIXER_STUDIO_IMAGE:-$DEFAULT_CAMERA_IMAGE}}"
  if ! docker_image_exists "$camera_image"; then
    err "Camera publisher image is not available locally: $camera_image"
    err "Pull it first with: docker pull $camera_image"
    exit 1
  fi
  container_name="crowdstage-camera-publisher-$$"
  media_command=(
    docker run --rm
    --init
    --name "$container_name"
    --device "$device"
    --group-add "$(stat -c '%g' "$device")"
    --entrypoint gst-launch-1.0
    "$camera_image"
    "${gst_args[@]}"
  )
fi

"${media_command[@]}" &
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
  info "Camera stream on ${device} stopped after ${timeout_seconds}s."
  exit 0
fi

exit "$status"
