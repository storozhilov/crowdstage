#!/usr/bin/env bash
#
# Ensure the BusMonitor Docker image exists.
#
# The same image is used by the mosaic and broadcast BusMonitor instances.
# The image is only built when it is missing, unless --force is given.
# ci/build.sh produces the BusMonitor bundle when it is missing or when the
# image is rebuilt with --force. The resulting image tag is printed to stdout.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DOCKERFILE="$REPO_ROOT/bus-monitor/Dockerfile"
readonly IMAGE="crowdstage-bus-monitor:dev"

force_build="$(force_requested "$@")" || exit 1

require_docker || exit 1

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing BusMonitor Dockerfile: $DOCKERFILE"
  exit 1
fi

if [[ "$force_build" -eq 0 ]] && docker_image_exists "$IMAGE"; then
  printf 'BusMonitor image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  if [[ "$force_build" -eq 1 || ! -f "$REPO_ROOT/bus-monitor/dist/index.html" ]]; then
    "$CI_DIR/build.sh" >&2
  fi
  build_docker_image "$DOCKERFILE" "$IMAGE" "$REPO_ROOT/bus-monitor"
fi

printf '%s\n' "$IMAGE"
