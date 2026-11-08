#!/usr/bin/env bash
#
# Ensure the end-to-end test image exists.
#
# The image is based on the pinned MosaicComposer runtime image and adds the Go
# toolchain and build dependencies, so media end-to-end tests can run in the
# same container as the GStreamer LiveKit plugins.
# The image is tagged for the current host user and group so that files written
# through container bind mounts are owned by the developer running the build.
# The image is only built when it is missing, unless --force is given.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly E2E_CONTEXT="$CI_DIR/e2e"
readonly DOCKERFILE="$E2E_CONTEXT/Dockerfile"
readonly IMAGE="crowdstage-e2e:uid${USER_ID}-gid${GROUP_ID}"

force_build="$(force_requested "$@")" || exit 1

require_docker || exit 1

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing end-to-end test Dockerfile: $DOCKERFILE"
  exit 1
fi

if [[ "$force_build" -eq 0 ]] && docker_image_exists "$IMAGE"; then
  printf 'End-to-end test image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  build_docker_image "$DOCKERFILE" "$IMAGE" "$E2E_CONTEXT"
fi

printf '%s\n' "$IMAGE"
