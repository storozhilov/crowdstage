#!/usr/bin/env bash
#
# Build (if needed) and deploy MosaicComposer to the development stand.
#
# The script starts the LiveKit service of the stand when it is not running.

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
# shellcheck source=../lib.sh
source "$SCRIPT_DIR/../lib.sh"

readonly SERVICE="mosaic-composer"

require_docker_compose || exit 1
ensure_env_file

"$SCRIPT_DIR/ensure_image.sh" >/dev/null

compose --profile mosaic up --detach --wait "$SERVICE"

load_env_file

printf 'MosaicComposer is deployed. Health endpoint: http://127.0.0.1:%s/health\n' "${MOSAIC_PORT:-7980}"
