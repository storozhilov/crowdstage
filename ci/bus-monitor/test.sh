#!/usr/bin/env bash
#
# Run BusMonitor unit and browser end-to-end tests inside the builder image.
#
# Usage: ./ci/bus-monitor/test.sh <mosaic|broadcast>
#
# The selected BusMonitor instance and its media source must be deployed to the
# development stand before this script runs, for example:
#   ./ci/dev.sh up --wait
#   ./ci/mosaic/deploy.sh
#   ./ci/media-mixer/deploy.sh
#   ./ci/bus-monitor/mosaic/deploy.sh
#   ./ci/bus-monitor/broadcast/deploy.sh
#   ./ci/bus-monitor/test.sh mosaic

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

instance="${1:-}"
case "$instance" in
  mosaic)
    monitor_url="$BUS_MONITOR_MOSAIC_URL"
    control_url="$MOSAIC_URL"
    room="${MOSAIC_TARGET_ROOM:-mosaic}"
    ;;
  broadcast)
    monitor_url="$BUS_MONITOR_BROADCAST_URL"
    control_url="$MEDIA_MIXER_URL"
    room="${MEDIA_MIXER_TARGET_ROOM:-broadcast}"
    ;;
  *)
    err "Usage: ${BASH_SOURCE[0]##*/} <mosaic|broadcast>"
    exit 2
    ;;
esac
readonly monitor_url control_url room

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE

mkdir -p "$NPM_CACHE_DIR"

printf 'Running BusMonitor (%s) tests\n' "$instance"
docker run \
  --rm \
  --init \
  --network host \
  --shm-size=1g \
  --mount "type=bind,source=$REPO_ROOT,target=/src" \
  --mount "type=bind,source=$NPM_CACHE_DIR,target=/cache/npm" \
  --env NPM_CONFIG_CACHE=/cache/npm \
  --env BUS_MONITOR_INSTANCE="$instance" \
  --env BUS_MONITOR_URL="$monitor_url" \
  --env BUS_MONITOR_ROOM="$room" \
  --env BUS_MONITOR_CONTROL_URL="$control_url" \
  --workdir /src/bus-monitor \
  "$BUILDER_IMAGE" \
  bash -c "npm ci && npm run test:unit && npm run test:e2e"
