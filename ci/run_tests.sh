#!/usr/bin/env bash
#
# Run the CrowdStage Go tests inside the builder container.
#
# Any arguments are passed to "go test", for example:
#   ./ci/run_tests.sh -run TestGreeting -v
#
# The Go module cache and build cache are kept in .cache/ on the host so that
# repeated runs do not download modules or recompile unchanged packages.
#
# Tests run against LiveKit and MosaicComposer on the development stand. Start
# the stand and deploy MosaicComposer first, for example:
#   ./ci/dev.sh up --wait
#   ./ci/mosaic/deploy.sh
#   ./ci/run_tests.sh

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

readonly CACHE_DIR="$REPO_ROOT/.cache"
readonly MODULE_CACHE_DIR="$CACHE_DIR/go-mod"
readonly BUILD_CACHE_DIR="$CACHE_DIR/go-build"

readonly BUILDER_IMAGE="$("$SCRIPT_DIR/ensure_builder.sh")"

load_env_file || true

mkdir -p "$MODULE_CACHE_DIR" "$BUILD_CACHE_DIR"

export LIVEKIT_URL="${LIVEKIT_URL:-ws://127.0.0.1:${LIVEKIT_PORT:-7880}}"
export MOSAIC_URL="${MOSAIC_URL:-http://127.0.0.1:${MOSAIC_PORT:-7980}}"
export MOSAIC_ROOM="${MOSAIC_ROOM:-mosaic-composer}"
export MOSAIC_STUDIO_ROOM="${MOSAIC_STUDIO_ROOM:-studio}"
export MOSAIC_IDENTITY="${MOSAIC_IDENTITY:-mosaic-composer}"

for module in app mosaic; do
  printf 'Running tests in %s\n' "$module"
  docker run \
    --rm \
    --network host \
    --mount "type=bind,source=$REPO_ROOT,target=/src" \
    --mount "type=bind,source=$MODULE_CACHE_DIR,target=/cache/go-mod" \
    --mount "type=bind,source=$BUILD_CACHE_DIR,target=/cache/go-build" \
    --env GOMODCACHE=/cache/go-mod \
    --env GOCACHE=/cache/go-build \
    --env LIVEKIT_URL \
    --env LIVEKIT_API_KEY \
    --env LIVEKIT_API_SECRET \
    --env MOSAIC_URL \
    --env MOSAIC_ROOM \
    --env MOSAIC_STUDIO_ROOM \
    --env MOSAIC_IDENTITY \
    --workdir "/src/$module" \
    "$BUILDER_IMAGE" \
    go test ./... "$@"
done
