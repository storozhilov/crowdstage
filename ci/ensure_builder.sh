#!/usr/bin/env bash
#
# Ensure the Go builder image used by ci/build.sh exists.
#
# The image is tagged for the current host user and group so that files written
# through container bind mounts are owned by the developer running the build.
# The image is only built when it is missing, unless --force is given.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

readonly BUILDER_CONTEXT="$SCRIPT_DIR/builder"
readonly DOCKERFILE="$BUILDER_CONTEXT/Dockerfile"
readonly USER_ID="$(id -u)"
readonly GROUP_ID="$(id -g)"
readonly IMAGE="crowdstage-builder:uid${USER_ID}-gid${GROUP_ID}"

force_build=0

for arg in "$@"; do
  case "$arg" in
    -f|--force)
      force_build=1
      ;;
    *)
      err "Unknown argument: $arg"
      printf 'Usage: %s [-f|--force]\n' "${BASH_SOURCE[0]##*/}" >&2
      exit 1
      ;;
  esac
done

require_docker || exit 1

if [[ ! -f "$DOCKERFILE" ]]; then
  err "Missing builder Dockerfile: $DOCKERFILE"
  exit 1
fi

if [[ "$force_build" -eq 0 ]] && docker image inspect "$IMAGE" >/dev/null 2>&1; then
  printf 'Builder image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  docker build \
    --file "$DOCKERFILE" \
    --tag "$IMAGE" \
    --build-arg "USER_ID=$USER_ID" \
    --build-arg "GROUP_ID=$GROUP_ID" \
    "$BUILDER_CONTEXT" >&2
fi

printf '%s\n' "$IMAGE"
