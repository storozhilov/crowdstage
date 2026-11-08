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

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE

mkdir -p "$MODULE_CACHE_DIR" "$BUILD_CACHE_DIR"

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
