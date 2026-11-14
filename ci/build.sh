#!/usr/bin/env bash
#
# Build the CrowdStage applications inside the builder container.
#
# The Go module cache and build cache are kept in .cache/ on the host so that
# repeated builds do not download modules or recompile unchanged packages.
# The npm cache is kept in .cache/npm for the same reason.
# Binaries are written to build/crowdstage, build/mosaic-composer, and
# build/media-mixer, and the BusMonitor and MixerConsole bundles are written to
# bus-monitor/dist and mixer-console/dist.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE

mkdir -p "$BUILD_DIR" "$MODULE_CACHE_DIR" "$BUILD_CACHE_DIR" "$NPM_CACHE_DIR"

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

build_bus_monitor() {
  printf 'Building bus-monitor\n'
  docker run --rm \
    --mount "type=bind,source=$REPO_ROOT,target=/src" \
    --mount "type=bind,source=$NPM_CACHE_DIR,target=/cache/npm" \
    --env NPM_CONFIG_CACHE=/cache/npm \
    --workdir /src/bus-monitor \
    "$BUILDER_IMAGE" \
    bash -c "npm ci && npm run check && npm run build"
}

build_mixer_console() {
  printf 'Building mixer-console\n'
  docker run --rm \
    --mount "type=bind,source=$REPO_ROOT,target=/src" \
    --mount "type=bind,source=$NPM_CACHE_DIR,target=/cache/npm" \
    --env NPM_CONFIG_CACHE=/cache/npm \
    --workdir /src/mixer-console \
    "$BUILDER_IMAGE" \
    bash -c "npm ci && npm run check && npm run build"
}

build_module app crowdstage 0
build_module mosaic mosaic-composer 1
build_module media-mixer media-mixer 1
build_bus_monitor
build_mixer_console
