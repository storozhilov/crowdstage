#!/usr/bin/env bash
#
# Ensure the Go builder image used by ci/build.sh exists.
#
# The image is tagged for the current host user and group so that files written
# through container bind mounts are owned by the developer running the build.
# The image is only built when it is missing, unless --force is given.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib.sh"

readonly BUILDER_CONTEXT="$CI_DIR/builder"
readonly DOCKERFILE="$BUILDER_CONTEXT/Dockerfile"
readonly IMAGE="crowdstage-builder:uid${USER_ID}-gid${GROUP_ID}"

force_build="$(force_requested "$@")" || exit 1

require_docker || exit 1

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing builder Dockerfile: $DOCKERFILE"
  exit 1
fi

if [[ "$force_build" -eq 0 ]] && docker_image_exists "$IMAGE"; then
  printf 'Builder image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  build_docker_image "$DOCKERFILE" "$IMAGE" "$BUILDER_CONTEXT"
fi

printf '%s\n' "$IMAGE"
