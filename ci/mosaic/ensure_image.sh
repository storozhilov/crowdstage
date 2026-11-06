#!/usr/bin/env bash
#
# Ensure the MosaicComposer Docker image exists.
#
# The image is only built when it is missing, unless --force is given.
# ci/build.sh produces the MosaicComposer binary when it is missing or when
# the image is rebuilt with --force.
# The resulting image tag is printed to stdout.

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
# shellcheck source=../lib.sh
source "$SCRIPT_DIR/../lib.sh"

readonly BUILD_DIR="$REPO_ROOT/build"
readonly DOCKERFILE="$REPO_ROOT/mosaic/Dockerfile"
readonly IMAGE="crowdstage-mosaic-composer:dev"

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
  err "Missing MosaicComposer Dockerfile: $DOCKERFILE"
  exit 1
fi

if [[ "$force_build" -eq 0 ]] && docker image inspect "$IMAGE" >/dev/null 2>&1; then
  printf 'MosaicComposer image %s already exists; skipping build.\n' "$IMAGE" >&2
else
  if [[ "$force_build" -eq 1 || ! -x "$BUILD_DIR/mosaic-composer" ]]; then
    "$REPO_ROOT/ci/build.sh" >&2
  fi
  docker build \
    --file "$DOCKERFILE" \
    --tag "$IMAGE" \
    "$BUILD_DIR" >&2
fi

printf '%s\n' "$IMAGE"
