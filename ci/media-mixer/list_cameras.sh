#!/usr/bin/env bash
#
# List virtual cameras configured by ci/media-mixer/setup_camera.sh.
#
# Usage:
#   ./ci/media-mixer/list_cameras.sh
#
# Each camera is printed as "<device><TAB><label>". The script only reports
# cameras whose label starts with MEDIA_MIXER_CAMERA_LABEL_PREFIX, so unrelated
# v4l2loopback devices are not included.
#
# Environment overrides:
#   MEDIA_MIXER_CAMERA_LABEL_PREFIX  Browser-visible camera name prefix
#   MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR  sysfs video device class directory
#   MEDIA_MIXER_CAMERA_DEVICE_DIR    Device directory

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_LABEL_PREFIX="CrowdStage Camera"
readonly VIDEO_CLASS_DIR="${MEDIA_MIXER_CAMERA_VIDEO_CLASS_DIR:-/sys/class/video4linux}"
readonly DEVICE_DIR="${MEDIA_MIXER_CAMERA_DEVICE_DIR:-/dev}"

usage() {
  printf 'Usage: %s\n' "${BASH_SOURCE[0]##*/}" >&2
  printf 'Lists configured virtual cameras as "<device><TAB><label>".\n' >&2
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      err "Unknown argument: $1"
      usage
      exit 2
      ;;
  esac
done

label_prefix="${MEDIA_MIXER_CAMERA_LABEL_PREFIX:-$DEFAULT_LABEL_PREFIX}"

for name_file in "$VIDEO_CLASS_DIR"/*/name; do
  [[ -e "$name_file" ]] || continue

  label="$(cat "$name_file")"
  if [[ "$label" != "$label_prefix "* ]]; then
    continue
  fi

  device_name="$(basename -- "$(dirname -- "$name_file")")"
  device="$DEVICE_DIR/$device_name"
  if [[ ! -e "$device" ]]; then
    continue
  fi

  printf '%s\t%s\n' "$device" "$label"
done | sort -V
