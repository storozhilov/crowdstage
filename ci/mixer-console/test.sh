#!/usr/bin/env bash
#
# Run MixerConsole unit and browser end-to-end tests inside the builder image.
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

mkdir -p "$NPM_CACHE_DIR"

printf 'Running MixerConsole tests\n'
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
  bash -c "npm ci && npm run test:unit && npm run test:e2e"
