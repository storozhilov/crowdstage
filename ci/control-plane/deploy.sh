#!/usr/bin/env bash
#
# Build (if needed) and deploy ControlPlane to the development stand.
#
# ControlPlane is the REST backend for MixerConsole and BusMonitor. The script
# starts the LiveKit service of the stand when it is not running.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="control-plane"

require_docker_compose || exit 1
ensure_env_file

"$CONTROL_PLANE_DIR/ensure_image.sh" >/dev/null

compose --profile control-plane up --detach --wait "$SERVICE"

printf 'ControlPlane is deployed: %s/\n' "$CONTROL_PLANE_URL"
