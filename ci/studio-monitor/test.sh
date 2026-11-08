#!/usr/bin/env bash
#
# Run StudioMonitor unit and browser end-to-end tests inside the builder image.
#
# StudioMonitor and MosaicComposer must be deployed to the development stand
# before this script runs, for example:
#   ./ci/dev.sh up --wait
#   ./ci/mosaic/deploy.sh
#   ./ci/studio-monitor/deploy.sh
#   ./ci/studio-monitor/test.sh

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE

mkdir -p "$NPM_CACHE_DIR"

printf 'Running StudioMonitor tests\n'
docker run \
  --rm \
  --init \
  --network host \
  --shm-size=1g \
  --mount "type=bind,source=$REPO_ROOT,target=/src" \
  --mount "type=bind,source=$NPM_CACHE_DIR,target=/cache/npm" \
  --env NPM_CONFIG_CACHE=/cache/npm \
  --env STUDIO_MONITOR_URL \
  --env MOSAIC_URL \
  --workdir /src/studio-monitor \
  "$BUILDER_IMAGE" \
  bash -c "npm ci && npm run test:unit && npm run test:e2e"
