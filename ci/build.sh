#!/usr/bin/env bash
#
# Build the CrowdStage Go applications inside the builder container.
#
# The Go module cache and build cache are kept in .cache/ on the host so that
# repeated builds do not download modules or recompile unchanged packages.
# Binaries are written to build/crowdstage and build/mosaic-composer.

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

readonly BUILD_DIR="$REPO_ROOT/build"
readonly CACHE_DIR="$REPO_ROOT/.cache"
readonly MODULE_CACHE_DIR="$CACHE_DIR/go-mod"
readonly BUILD_CACHE_DIR="$CACHE_DIR/go-build"

readonly BUILDER_IMAGE="$("$SCRIPT_DIR/ensure_builder.sh")"

mkdir -p "$BUILD_DIR" "$MODULE_CACHE_DIR" "$BUILD_CACHE_DIR"

build_module() {
  local module="$1"
  local binary="$2"

  printf 'Building %s\n' "$module"
  docker run --rm \
    --mount "type=bind,source=$REPO_ROOT,target=/src" \
    --mount "type=bind,source=$MODULE_CACHE_DIR,target=/cache/go-mod" \
    --mount "type=bind,source=$BUILD_CACHE_DIR,target=/cache/go-build" \
    --env CGO_ENABLED=0 \
    --env GOMODCACHE=/cache/go-mod \
    --env GOCACHE=/cache/go-build \
    --workdir "/src/$module" \
    "$BUILDER_IMAGE" \
    bash -c "go build -trimpath -o /src/build/$binary ."
}

build_module app crowdstage
build_module mosaic mosaic-composer
