#!/usr/bin/env bash
#
# Uninstall a BusMonitor instance from the development stand.
#
# Usage: ./ci/bus-monitor/uninstall.sh <mosaic|broadcast>
#
# The LiveKit service and the BusMonitor image are kept intact.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

instance="${1:-}"
case "$instance" in
  mosaic)
    service="bus-monitor-mosaic"
    ;;
  broadcast)
    service="bus-monitor-broadcast"
    ;;
  *)
    err "Usage: ${BASH_SOURCE[0]##*/} <mosaic|broadcast>"
    exit 2
    ;;
esac
readonly service

require_docker_compose || exit 1
ensure_env_file

container_ids="$(compose --profile bus-monitor ps --quiet --all "$service")"
if [[ -z "$container_ids" ]]; then
  printf 'BusMonitor (%s) is not deployed; nothing to uninstall.\n' "$instance"
  exit 0
fi

compose --profile bus-monitor rm --stop --force "$service"

printf 'BusMonitor (%s) has been uninstalled.\n' "$instance"
