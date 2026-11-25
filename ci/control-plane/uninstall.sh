#!/usr/bin/env bash
#
# Uninstall ControlPlane from the development stand.
#
# The LiveKit service and the ControlPlane image are kept intact.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="control-plane"

require_docker_compose || exit 1
ensure_env_file

container_ids="$(compose --profile control-plane ps --quiet --all "$SERVICE")"
if [[ -z "$container_ids" ]]; then
  printf 'ControlPlane is not deployed; nothing to uninstall.\n'
  exit 0
fi

compose --profile control-plane rm --stop --force "$SERVICE"

printf 'ControlPlane has been uninstalled.\n'
