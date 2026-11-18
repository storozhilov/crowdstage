#!/usr/bin/env bash
#
# Remove a particular virtual camera configured by
# ci/media-mixer/setup_camera.sh.
#
# Usage:
#   ./ci/media-mixer/remove_camera.sh TARGET
#
# TARGET is one of the values printed by list_cameras.sh: the 1-based camera
# index, the device path, the label, or a bare "videoN" device name. The
# remaining virtual cameras keep their device numbers and labels. Removing the
# last camera unloads the v4l2loopback module.
#
# When run on the host, the script re-executes itself in a privileged container
# with the host kernel modules and devices mounted, because it needs root
# privileges. Inside a container it runs directly as root. Producers using any
# of the virtual cameras must be stopped first.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEVICE_DIR="${MEDIA_MIXER_CAMERA_DEVICE_DIR:-/dev}"

usage() {
  printf 'Usage: %s TARGET\n' "${BASH_SOURCE[0]##*/}" >&2
  printf 'TARGET is a camera index, device path, device name, or label.\n' >&2
}

run_in_container() {
  require_docker || exit 1

  local image
  image="$("$CI_DIR/ensure_builder.sh")" || exit 1

  local -a env_args=(--env MEDIA_MIXER_CAMERA_IN_CONTAINER=1)
  local name
  for name in \
    MEDIA_MIXER_CAMERA_LABEL_PREFIX \
    MEDIA_MIXER_CAMERA_SYS_MODULE_DIR \
    MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR \
    MEDIA_MIXER_CAMERA_DEVICE_DIR; do
    if [[ -n "${!name+x}" ]]; then
      env_args+=(--env "$name")
    fi
  done

  docker run \
    --rm \
    --privileged \
    --network none \
    --user 0 \
    --mount "type=bind,source=/lib/modules,target=/lib/modules,readonly" \
    --mount "type=bind,source=/dev,target=/dev" \
    --mount "type=bind,source=$REPO_ROOT,target=/src,readonly" \
    "${env_args[@]}" \
    --entrypoint bash \
    "$image" \
    "/src/ci/media-mixer/${BASH_SOURCE[0]##*/}" "$@"
}

target=""
target_set=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    -d|--device)
      if [[ "$#" -lt 2 ]]; then
        err "Option $1 requires a camera target."
        usage
        exit 2
      fi
      if (( target_set == 1 )); then
        err "Camera target was provided more than once."
        exit 2
      fi
      target="$2"
      target_set=1
      shift 2
      ;;
    -*)
      err "Unknown argument: $1"
      usage
      exit 2
      ;;
    *)
      if (( target_set == 1 )); then
        err "Camera target was provided more than once: $target and $1"
        exit 2
      fi
      target="$1"
      target_set=1
      shift
      ;;
  esac
done

if (( target_set == 0 )) || [[ -z "$target" ]]; then
  err "A camera target is required."
  usage
  exit 2
fi

inside_container=0
if [[ -f /.dockerenv || "${MEDIA_MIXER_CAMERA_IN_CONTAINER:-0}" == "1" ]]; then
  inside_container=1
fi
if (( inside_container == 0 )); then
  run_in_container "$target"
  exit $?
fi

if [[ "$(id -u)" != "0" ]]; then
  err "remove_camera.sh must run as root inside the camera container."
  exit 1
fi

require_command modprobe

mapfile -t camera_lines < <("$MEDIA_MIXER_DIR/list_cameras.sh")
if (( ${#camera_lines[@]} == 0 )); then
  err "No virtual cameras are configured."
  exit 1
fi

camera_devices=()
camera_labels=()
camera_video_numbers=()
for line in "${camera_lines[@]}"; do
  device="${line%%$'\t'*}"
  label="${line#*$'\t'}"
  video_name="${device##*/}"
  if [[ ! "$video_name" =~ ^video([0-9]+)$ ]]; then
    err "Unexpected virtual camera device: $device"
    exit 1
  fi
  camera_devices+=("$device")
  camera_labels+=("$label")
  camera_video_numbers+=("${BASH_REMATCH[1]}")
done

target_index=-1
if [[ "$target" =~ ^[0-9]+$ ]]; then
  requested_index=$((10#$target))
  if (( requested_index < 1 || requested_index > ${#camera_devices[@]} )); then
    err "Camera index is out of range: $target (configured: ${#camera_devices[@]})"
    exit 1
  fi
  target_index=$((requested_index - 1))
else
  normalized_target="$target"
  if [[ "$target" =~ ^video[0-9]+$ || "$target" =~ ^/dev/video[0-9]+$ ]]; then
    normalized_target="$DEVICE_DIR/${target##*/}"
  fi

  for index in "${!camera_devices[@]}"; do
    if [[ "${camera_devices[$index]}" == "$normalized_target" || \
          "${camera_labels[$index]}" == "$target" ]]; then
      target_index="$index"
      break
    fi
  done
fi

if (( target_index < 0 )); then
  err "Virtual camera not found: $target"
  err "Configured cameras:"
  printf '%s\n' "${camera_lines[@]}" >&2
  exit 1
fi

removed_device="${camera_devices[$target_index]}"
removed_label="${camera_labels[$target_index]}"

remaining_devices=()
remaining_labels=()
remaining_video_numbers=()
remaining_exclusive_caps=()
for index in "${!camera_devices[@]}"; do
  if (( index == target_index )); then
    continue
  fi
  remaining_devices+=("${camera_devices[$index]}")
  remaining_labels+=("${camera_labels[$index]}")
  remaining_video_numbers+=("${camera_video_numbers[$index]}")
  remaining_exclusive_caps+=(1)
done

if ! modprobe -r v4l2loopback; then
  err "Could not unload v4l2loopback. Stop camera producers using it and try again."
  exit 1
fi

if (( ${#remaining_devices[@]} == 0 )); then
  info "Removed virtual camera ${removed_device} (${removed_label}); no virtual cameras remain." >&2
  printf '%s\n' "$removed_device"
  exit 0
fi

video_nr_list="$(IFS=,; printf '%s' "${remaining_video_numbers[*]}")"
label_list="$(IFS=,; printf '%s' "${remaining_labels[*]}")"
exclusive_caps_list="$(IFS=,; printf '%s' "${remaining_exclusive_caps[*]}")"

info "Removing virtual camera ${removed_device} (${removed_label})." >&2
if ! modprobe v4l2loopback \
  devices="${#remaining_devices[@]}" \
  video_nr="$video_nr_list" \
  card_label="$label_list" \
  exclusive_caps="$exclusive_caps_list" \
  max_buffers=2; then
  err "Could not reconfigure the remaining virtual cameras."
  exit 1
fi

deadline=$((SECONDS + 5))
for device in "${remaining_devices[@]}"; do
  while [[ ! -e "$device" ]]; do
    if (( SECONDS >= deadline )); then
      err "Virtual camera device did not reappear: $device"
      exit 1
    fi
    sleep 0.1
  done
done

printf '%s\n' "$removed_device"
