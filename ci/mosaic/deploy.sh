#!/usr/bin/env bash
#
# Build (if needed) and deploy MosaicComposer to the development stand.
#
# The script starts the LiveKit service of the stand when it is not running.

set -Eeuo pipefail

# shellcheck source=../lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="mosaic-composer"

require_docker_compose || exit 1
ensure_env_file

"$MOSAIC_DIR/ensure_image.sh" >/dev/null

compose --profile mosaic up --detach --wait "$SERVICE"

printf 'MosaicComposer is deployed. Health endpoint: %s/health\n' "$MOSAIC_URL"
