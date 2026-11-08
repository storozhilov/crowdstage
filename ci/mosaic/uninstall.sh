#!/usr/bin/env bash
#
# Uninstall MosaicComposer from the development stand.
#
# The LiveKit service and the MosaicComposer image are kept intact.

set -Eeuo pipefail

# shellcheck source=../lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="mosaic-composer"

require_docker_compose || exit 1
ensure_env_file

container_ids="$(compose --profile mosaic ps --quiet --all "$SERVICE")"
if [[ -z "$container_ids" ]]; then
  printf 'MosaicComposer is not deployed; nothing to uninstall.\n'
  exit 0
fi

compose --profile mosaic rm --stop --force "$SERVICE"

printf 'MosaicComposer has been uninstalled.\n'
