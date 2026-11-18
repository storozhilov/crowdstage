#!/usr/bin/env bash
#
# Configure virtual V4L2 loopback cameras that web browsers detect as webcams.
#
# Usage:
#   ./ci/media-mixer/setup_camera.sh [COUNT]
#
# COUNT defaults to MEDIA_MIXER_CAMERA_COUNT or 1. The script loads the
# v4l2loopback kernel module and prints one device path per camera on stdout, so
# callers can pass them to run_camera.sh.
#
# Environment overrides:
#   MEDIA_MIXER_CAMERA_COUNT         Number of cameras when COUNT is omitted
#   MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR  First /dev/videoN number (default: 10)
#   MEDIA_MIXER_CAMERA_LABEL_PREFIX  Browser-visible camera name prefix
#
# The script must run as root. In the MixerConsole end-to-end tests it runs in a
# privileged container with the host kernel modules available.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_FIRST_VIDEO_NR=10
readonly DEFAULT_LABEL_PREFIX="CrowdStage Camera"
readonly SYS_MODULE_DIR="${MEDIA_MIXER_CAMERA_SYS_MODULE_DIR:-/sys/module}"
readonly VIDEO_CLASS_DIR="${MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR:-/sys/class/video4linux}"
readonly DEVICE_DIR="${MEDIA_MIXER_CAMERA_DEVICE_DIR:-/dev}"

usage() {
  printf 'Usage: %s [COUNT]\n' "${BASH_SOURCE[0]##*/}" >&2
  printf 'Configures COUNT virtual cameras and prints their device paths.\n' >&2
}

count="${MEDIA_MIXER_CAMERA_COUNT:-1}"
count_set=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      if [[ ! "$1" =~ ^[0-9]+$ ]]; then
        err "Camera count must be a positive integer: $1"
        usage
        exit 2
      fi
      if (( count_set == 1 )); then
        err "Camera count was provided more than once."
        usage
        exit 2
      fi
      count="$1"
      count_set=1
      shift
      ;;
  esac
done

count=$((10#$count))
if (( count <= 0 )); then
  err "Camera count must be a positive integer: $count"
  exit 2
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

if [[ "$(id -u)" != "0" ]]; then
  err "setup_camera.sh must run as root (for example in a privileged container)."
  exit 1
fi

require_command modprobe

expected_labels=()
expected_video_numbers=()
expected_devices=()
expected_exclusive_caps=()
for (( index = 1; index <= count; index++ )); do
  expected_labels+=("$label_prefix $index")
  expected_video_numbers+=("$((first_video_nr + index - 1))")
  expected_devices+=("$DEVICE_DIR/video$((first_video_nr + index - 1))")
  expected_exclusive_caps+=(1)
done

label_list="$(IFS=,; printf '%s' "${expected_labels[*]}")"
video_nr_list="$(IFS=,; printf '%s' "${expected_video_numbers[*]}")"
exclusive_caps_list="$(IFS=,; printf '%s' "${expected_exclusive_caps[*]}")"

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

configured_devices=()
all_configured=1
for label in "${expected_labels[@]}"; do
  if ! device="$(device_for_label "$label")" || [[ ! -e "$device" ]]; then
    all_configured=0
    break
  fi
  configured_devices+=("$device")
done

exclusive_caps_configured=0
exclusive_caps_parameter="$SYS_MODULE_DIR/v4l2loopback/parameters/exclusive_caps"
if [[ -f "$exclusive_caps_parameter" ]]; then
  IFS=, read -r -a current_exclusive_caps <<<"$(cat "$exclusive_caps_parameter")"
  if (( ${#current_exclusive_caps[@]} >= count )); then
    exclusive_caps_configured=1
    for (( index = 0; index < count; index++ )); do
      case "${current_exclusive_caps[$index]}" in
        Y|y|1) ;;
        *) exclusive_caps_configured=0 ;;
      esac
    done
  fi
fi

if (( all_configured == 1 && exclusive_caps_configured == 1 )); then
  printf '%s\n' "${configured_devices[@]}"
  exit 0
fi

if [[ -d "$SYS_MODULE_DIR/v4l2loopback" ]]; then
  info "Reloading v4l2loopback to configure ${count} camera(s)." >&2
  if ! modprobe -r v4l2loopback; then
    err "v4l2loopback is already loaded with different settings and cannot be unloaded."
    err "Stop the processes using it and run setup_camera.sh again."
    exit 1
  fi
fi

info "Configuring ${count} virtual camera(s): ${label_list}" >&2
if ! modprobe v4l2loopback \
  devices="$count" \
  video_nr="$video_nr_list" \
  card_label="$label_list" \
  exclusive_caps="$exclusive_caps_list" \
  max_buffers=2; then
  err "Could not load the v4l2loopback kernel module."
  err "Install the module on the host (for example: sudo apt-get install v4l2loopback-dkms)."
  exit 1
fi

deadline=$((SECONDS + 5))
for device in "${expected_devices[@]}"; do
  while [[ ! -e "$device" ]]; do
    if (( SECONDS >= deadline )); then
      err "Virtual camera device did not appear: $device"
      exit 1
    fi
    sleep 0.1
  done
done

printf '%s\n' "${expected_devices[@]}"
