#!/usr/bin/env bash
#
# Uninstall StudioMonitor from the development stand.
#
# The LiveKit service and the StudioMonitor image are kept intact.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="studio-monitor"

require_docker_compose || exit 1
ensure_env_file

container_ids="$(compose --profile studio-monitor ps --quiet --all "$SERVICE")"
if [[ -z "$container_ids" ]]; then
  printf 'StudioMonitor is not deployed; nothing to uninstall.\n'
  exit 0
fi

compose --profile studio-monitor rm --stop --force "$SERVICE"

printf 'StudioMonitor has been uninstalled.\n'
