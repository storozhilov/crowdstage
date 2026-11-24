#!/usr/bin/env bash
#
# Ensure the ControlPlane Docker image exists.
#
# The image is only built when it is missing, unless --force is given.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DOCKERFILE="$REPO_ROOT/control-plane/Dockerfile"
readonly IMAGE="crowdstage-control-plane:dev"

force_build="$(force_requested "$@")" || exit 1

require_docker || exit 1

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing ControlPlane Dockerfile: $DOCKERFILE"
  exit 1
fi

if [[ "$force_build" -eq 0 ]] && docker_image_exists "$IMAGE"; then
  printf 'ControlPlane image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  build_docker_image "$DOCKERFILE" "$IMAGE" "$REPO_ROOT/control-plane"
fi

printf '%s\n' "$IMAGE"
