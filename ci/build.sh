#!/usr/bin/env bash
#
# Build the CrowdStage Go applications inside the builder container.
#
# The Go module cache and build cache are kept in .cache/ on the host so that
# repeated builds do not download modules or recompile unchanged packages.
# Binaries are written to build/crowdstage and build/mosaic-composer.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE

mkdir -p "$BUILD_DIR" "$MODULE_CACHE_DIR" "$BUILD_CACHE_DIR"

build_module() {
  local module="$1"
  local binary="$2"
  local cgo_enabled="$3"

  printf 'Building %s\n' "$module"
  docker run --rm \
    --mount "type=bind,source=$REPO_ROOT,target=/src" \
    --mount "type=bind,source=$MODULE_CACHE_DIR,target=/cache/go-mod" \
    --mount "type=bind,source=$BUILD_CACHE_DIR,target=/cache/go-build" \
    --env "CGO_ENABLED=$cgo_enabled" \
    --env GOMODCACHE=/cache/go-mod \
    --env GOCACHE=/cache/go-build \
    --workdir "/src/$module" \
    "$BUILDER_IMAGE" \
    bash -c "go build -trimpath -o /src/build/$binary ."
}

build_module app crowdstage 0
build_module mosaic mosaic-composer 1
