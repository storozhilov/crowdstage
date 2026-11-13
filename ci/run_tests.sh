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
# Tests run against LiveKit, MosaicComposer, and MediaMixer on the development
# stand. Start the stand and deploy the components first, for example:
#   ./ci/dev.sh up --wait
#   ./ci/mosaic/deploy.sh
#   ./ci/media-mixer/deploy.sh
#   ./ci/run_tests.sh

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
E2E_IMAGE="$("$CI_DIR/e2e/ensure_image.sh")"
readonly BUILDER_IMAGE E2E_IMAGE
readonly GUEST_TESTS='^TestMosaicComposer(ShowsGuestOverlayAcrossJoinDisconnectCycles|ShowsGuestGridAcrossRandomJoinLeave|RotatesGuestsThroughSmallGrid)$'
readonly MEDIA_MIXER_TESTS='^TestMediaMixerForwardsOldestStudioVideo$'

mkdir -p "$MODULE_CACHE_DIR" "$BUILD_CACHE_DIR" "$E2E_BUILD_CACHE_DIR"

common_docker_args=(
  --rm
  --network host
  --mount "type=bind,source=$REPO_ROOT,target=/src"
  --mount "type=bind,source=$MODULE_CACHE_DIR,target=/cache/go-mod"
  --env GOMODCACHE=/cache/go-mod
  --env LIVEKIT_URL
  --env LIVEKIT_API_KEY
  --env LIVEKIT_API_SECRET
  --env MOSAIC_URL
  --env MOSAIC_SOURCE_ROOM
  --env MOSAIC_TARGET_ROOM
  --env MOSAIC_IDENTITY
  --env MEDIA_MIXER_SOURCE_ROOM
)

for module in app shared mosaic media-mixer; do
  printf 'Running tests in %s\n' "$module"
  docker run \
    "${common_docker_args[@]}" \
    --mount "type=bind,source=$BUILD_CACHE_DIR,target=/cache/go-build" \
    --env GOCACHE=/cache/go-build \
    --workdir "/src/$module" \
    "$BUILDER_IMAGE" \
    go test ./... -skip "$GUEST_TESTS" "$@"
done

printf 'Running guest media test in e2e\n'
docker run \
  "${common_docker_args[@]}" \
  --mount "type=bind,source=$E2E_BUILD_CACHE_DIR,target=/cache/go-build" \
  --env GOCACHE=/cache/go-build \
  --env MOSAIC_GUEST_SCRIPT=/src/ci/mosaic/run_guest.sh \
  --workdir /src/mosaic \
  "$E2E_IMAGE" \
  go test ./... -run "$GUEST_TESTS" "$@"

printf 'Running media mixer picture test in e2e\n'
docker run \
  "${common_docker_args[@]}" \
  --mount "type=bind,source=$E2E_BUILD_CACHE_DIR,target=/cache/go-build" \
  --env GOCACHE=/cache/go-build \
  --env MEDIA_MIXER_URL \
  --env MEDIA_MIXER_SOURCE_ROOM \
  --env MEDIA_MIXER_TARGET_ROOM \
  --env MEDIA_MIXER_IDENTITY \
  --env MEDIA_MIXER_STUDIO_SCRIPT=/src/ci/media-mixer/run_studio.sh \
  --workdir /src/media-mixer \
  "$E2E_IMAGE" \
  go test ./... -run "$MEDIA_MIXER_TESTS" "$@"
