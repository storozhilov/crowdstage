#!/usr/bin/env bash
#
# Build (if needed) and deploy a BusMonitor instance to the development stand.
#
# Usage: ./ci/bus-monitor/deploy.sh <mosaic|broadcast>
#
# The "mosaic" instance monitors the room where MosaicComposer publishes its
# composed output. The "broadcast" instance monitors the room where MediaMixer
# publishes the main mix picture.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

instance="${1:-}"
case "$instance" in
  mosaic)
    service="bus-monitor-mosaic"
    url="$BUS_MONITOR_MOSAIC_URL"
    ;;
  broadcast)
    service="bus-monitor-broadcast"
    url="$BUS_MONITOR_BROADCAST_URL"
    ;;
  *)
    err "Usage: ${BASH_SOURCE[0]##*/} <mosaic|broadcast>"
    exit 2
    ;;
esac
readonly service url

require_docker_compose || exit 1
ensure_env_file

"$BUS_MONITOR_DIR/ensure_image.sh" >/dev/null

compose --profile bus-monitor up --detach --wait "$service"

printf 'BusMonitor (%s) is deployed: %s/\n' "$instance" "$url"
