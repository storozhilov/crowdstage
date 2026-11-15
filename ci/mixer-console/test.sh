#!/usr/bin/env bash
#
# Run MixerConsole unit and browser end-to-end tests inside the builder image.
#
# Four studio test publishers are started with ci/media-mixer/run_studio.sh so
# the Studio panel has enough live sources to fill its card row and exercise the
# carousel navigation. The runtime image is pulled when missing.
#
# The stand, MosaicComposer, MediaMixer, and MixerConsole must be deployed
# before this script runs, for example:
#   ./ci/dev.sh up --wait
#   ./ci/mosaic/deploy.sh
#   ./ci/media-mixer/deploy.sh
#   ./ci/mixer-console/deploy.sh
#   ./ci/mixer-console/test.sh

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE
readonly STUDIO_PUBLISHER="$REPO_ROOT/ci/media-mixer/run_studio.sh"
readonly STUDIO_PUBLISH_TIMEOUT_SECONDS=300
readonly STUDIO_PUBLISHER_IDENTITY="mixer-console-test-$$"

studio_patterns=(smpte snow ball bar)
studio_publisher_pids=()
mkdir -p "$NPM_CACHE_DIR"

studio_publisher_log="$(mktemp "${TMPDIR:-/tmp}/crowdstage-mixer-console-studio.XXXXXX")"

# shellcheck disable=SC2317 # invoked by the EXIT trap
cleanup() {
  local pid
  if (( ${#studio_publisher_pids[@]} > 0 )); then
    for pid in "${studio_publisher_pids[@]}"; do
      kill -INT "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    done
  fi
  studio_publisher_pids=()
  rm -f "$studio_publisher_log"
}
trap cleanup EXIT

if ! docker_image_exists "$MEDIA_MIXER_STUDIO_IMAGE"; then
  info "Pulling studio publisher image $MEDIA_MIXER_STUDIO_IMAGE"
  docker pull "$MEDIA_MIXER_STUDIO_IMAGE"
fi

printf 'Starting %d studio test publishers\n' "${#studio_patterns[@]}"
for index in "${!studio_patterns[@]}"; do
  MEDIA_MIXER_STUDIO_IDENTITY="$STUDIO_PUBLISHER_IDENTITY-$((index + 1))" \
  MEDIA_MIXER_STUDIO_PATTERN="${studio_patterns[$index]}" \
    "$STUDIO_PUBLISHER" "$STUDIO_PUBLISH_TIMEOUT_SECONDS" \
    >>"$studio_publisher_log" 2>&1 &
  studio_publisher_pids+=("$!")
done

printf 'Running MixerConsole tests\n'
status=0
docker run \
  --rm \
  --init \
  --network host \
  --shm-size=1g \
  --mount "type=bind,source=$REPO_ROOT,target=/src" \
  --mount "type=bind,source=$NPM_CACHE_DIR,target=/cache/npm" \
  --env NPM_CONFIG_CACHE=/cache/npm \
  --env MIXER_CONSOLE_URL \
  --env LIVEKIT_URL \
  --env LIVEKIT_API_KEY \
  --env LIVEKIT_API_SECRET \
  --env "MIXER_CONSOLE_STUDIO_ROOM=$MIXER_CONSOLE_STUDIO_ROOM" \
  --env MOSAIC_SOURCE_ROOM \
  --env MOSAIC_TARGET_ROOM \
  --env MEDIA_MIXER_SOURCE_ROOM \
  --env MEDIA_MIXER_TARGET_ROOM \
  --env "STUDIO_PUBLISHER_IDENTITY=$STUDIO_PUBLISHER_IDENTITY" \
  --workdir /src/mixer-console \
  "$BUILDER_IMAGE" \
  bash -c "npm ci && npm run test:unit && npm run test:e2e" || status=$?

if [[ "$status" -ne 0 && -s "$studio_publisher_log" ]]; then
  printf 'Studio test publisher log:\n'
  cat "$studio_publisher_log"
fi

exit "$status"
