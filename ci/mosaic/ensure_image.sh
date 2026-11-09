#!/usr/bin/env bash
#
# Ensure the MosaicComposer Docker image exists.
#
# The image is only built when it is missing, unless --force is given.
# ci/build.sh produces the MosaicComposer binary when it is missing or when
# the image is rebuilt with --force. The MosaicComposer image also needs the
# locally built patched gst-plugin-webrtc image, which is rebuilt with --force.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DOCKERFILE="$REPO_ROOT/mosaic/Dockerfile"
readonly IMAGE="crowdstage-mosaic-composer:dev"
readonly GST_PLUGIN_CONTEXT="$REPO_ROOT/ci/mosaic/gst-plugin"
readonly GST_PLUGIN_DOCKERFILE="$GST_PLUGIN_CONTEXT/Dockerfile"
readonly GST_PLUGIN_IMAGE="crowdstage-gst-plugin-webrtc:0.14.5-patched"

force_build="$(force_requested "$@")" || exit 1

require_docker || exit 1

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing MosaicComposer Dockerfile: $DOCKERFILE"
  exit 1
fi
if [[ ! -f "$GST_PLUGIN_DOCKERFILE" ]]; then
  err "Missing patched GStreamer plugin Dockerfile: $GST_PLUGIN_DOCKERFILE"
  exit 1
fi

ensure_gst_plugin_image() {
  if [[ "$force_build" -eq 0 ]] && docker_image_exists "$GST_PLUGIN_IMAGE"; then
    printf 'Patched GStreamer plugin image %s already exists; skipping build.\n' "$GST_PLUGIN_IMAGE" >&2
  else
    build_docker_image "$GST_PLUGIN_DOCKERFILE" "$GST_PLUGIN_IMAGE" "$GST_PLUGIN_CONTEXT"
  fi
}

if [[ "$force_build" -eq 0 ]] && docker_image_exists "$IMAGE"; then
  printf 'MosaicComposer image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  ensure_gst_plugin_image
  if [[ "$force_build" -eq 1 || ! -x "$BUILD_DIR/mosaic-composer" ]]; then
    "$CI_DIR/build.sh" >&2
  fi
  build_docker_image "$DOCKERFILE" "$IMAGE" "$BUILD_DIR"
fi

printf '%s\n' "$IMAGE"
