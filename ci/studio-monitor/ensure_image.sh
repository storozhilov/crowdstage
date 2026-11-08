#!/usr/bin/env bash
#
# Ensure the StudioMonitor Docker image exists.
#
# The image is only built when it is missing, unless --force is given.
# ci/build.sh produces the StudioMonitor bundle when it is missing or when the
# image is rebuilt with --force.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DOCKERFILE="$REPO_ROOT/studio-monitor/Dockerfile"
readonly IMAGE="crowdstage-studio-monitor:dev"

force_build="$(force_requested "$@")" || exit 1

require_docker || exit 1

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing StudioMonitor Dockerfile: $DOCKERFILE"
  exit 1
fi

if [[ "$force_build" -eq 0 ]] && docker_image_exists "$IMAGE"; then
  printf 'StudioMonitor image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  if [[ "$force_build" -eq 1 || ! -f "$REPO_ROOT/studio-monitor/dist/index.html" ]]; then
    "$CI_DIR/build.sh" >&2
  fi
  build_docker_image "$DOCKERFILE" "$IMAGE" "$REPO_ROOT/studio-monitor"
fi

printf '%s\n' "$IMAGE"
