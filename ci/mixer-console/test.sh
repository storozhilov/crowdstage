#!/usr/bin/env bash
#
# Run MixerConsole unit and browser end-to-end tests inside the builder image.
#
# A studio test publisher is started with ci/media-mixer/run_studio.sh so the
# Studio panel has a live source. Its runtime image must already be available.
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

mkdir -p "$NPM_CACHE_DIR"

studio_publisher_pid=""
studio_publisher_log="$(mktemp "${TMPDIR:-/tmp}/crowdstage-mixer-console-studio.XXXXXX")"

# shellcheck disable=SC2317 # invoked by the EXIT trap
cleanup() {
  if [[ -n "$studio_publisher_pid" ]]; then
    kill -INT "$studio_publisher_pid" 2>/dev/null || true
    wait "$studio_publisher_pid" 2>/dev/null || true
    studio_publisher_pid=""
  fi
  rm -f "$studio_publisher_log"
}
trap cleanup EXIT

printf 'Starting studio test publisher\n'
MEDIA_MIXER_STUDIO_IDENTITY="mixer-console-test-$$" \
  "$STUDIO_PUBLISHER" "$STUDIO_PUBLISH_TIMEOUT_SECONDS" \
  >"$studio_publisher_log" 2>&1 &
studio_publisher_pid=$!

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
  --workdir /src/mixer-console \
  "$BUILDER_IMAGE" \
  bash -c "npm ci && npm run test:unit && npm run test:e2e" || status=$?

if [[ "$status" -ne 0 && -s "$studio_publisher_log" ]]; then
  printf 'Studio test publisher log:\n'
  cat "$studio_publisher_log"
fi

exit "$status"
