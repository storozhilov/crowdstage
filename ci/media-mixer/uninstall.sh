#!/usr/bin/env bash
#
# Uninstall MediaMixer from the development stand.
#
# The LiveKit service and the MediaMixer image are kept intact.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="media-mixer"

require_docker_compose || exit 1
ensure_env_file

container_ids="$(compose --profile media-mixer ps --quiet --all "$SERVICE")"
if [[ -z "$container_ids" ]]; then
  printf 'MediaMixer is not deployed; nothing to uninstall.\n'
  exit 0
fi

compose --profile media-mixer rm --stop --force "$SERVICE"

printf 'MediaMixer has been uninstalled.\n'
