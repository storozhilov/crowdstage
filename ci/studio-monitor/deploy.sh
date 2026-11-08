#!/usr/bin/env bash
#
# Build (if needed) and deploy StudioMonitor to the development stand.
#
# The script starts the LiveKit service of the stand when it is not running.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="studio-monitor"

require_docker_compose || exit 1
ensure_env_file

"$STUDIO_MONITOR_DIR/ensure_image.sh" >/dev/null

compose --profile studio-monitor up --detach --wait "$SERVICE"

printf 'StudioMonitor is deployed: %s/\n' "$STUDIO_MONITOR_URL"
