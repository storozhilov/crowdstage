#!/usr/bin/env bash
#
# Uninstall MixerConsole from the development stand.
#
# The LiveKit service and the MixerConsole image are kept intact.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="mixer-console"

require_docker_compose || exit 1
ensure_env_file

container_ids="$(compose --profile mixer-console ps --quiet --all "$SERVICE")"
if [[ -z "$container_ids" ]]; then
  printf 'MixerConsole is not deployed; nothing to uninstall.\n'
  exit 0
fi

compose --profile mixer-console rm --stop --force "$SERVICE"

printf 'MixerConsole has been uninstalled.\n'
