#!/usr/bin/env bash
#
# Build the Go builder image used by ci/build.sh.
#
# The image is tagged for the current host user and group so that files written
# through container bind mounts are owned by the developer running the build.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
readonly BUILDER_CONTEXT="$SCRIPT_DIR/builder"
readonly DOCKERFILE="$BUILDER_CONTEXT/Dockerfile"
readonly USER_ID="$(id -u)"
readonly GROUP_ID="$(id -g)"
readonly IMAGE="crowdstage-builder:uid${USER_ID}-gid${GROUP_ID}"

err() {
  printf 'Error: %s\n' "$*" >&2
}

if ! command -v docker >/dev/null 2>&1; then
  err "Docker is not installed or is not available in PATH."
  printf '%s\n' \
    'Install Docker Engine and Docker Compose V2, then try again:' \
    '  https://docs.docker.com/engine/install/' >&2
  exit 1
fi

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing builder Dockerfile: $DOCKERFILE"
  exit 1
fi

docker build \
  --file "$DOCKERFILE" \
  --tag "$IMAGE" \
  --build-arg "USER_ID=$USER_ID" \
  --build-arg "GROUP_ID=$GROUP_ID" \
  "$BUILDER_CONTEXT" >&2

printf '%s\n' "$IMAGE"
