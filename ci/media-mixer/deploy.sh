#!/usr/bin/env bash
#
# Build (if needed) and deploy MediaMixer to the development stand.
#
# The script starts the LiveKit service of the stand when it is not running.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="media-mixer"

require_docker_compose || exit 1
ensure_env_file

"$MEDIA_MIXER_DIR/ensure_image.sh" >/dev/null

compose --profile media-mixer up --detach --wait "$SERVICE"

printf 'MediaMixer is deployed. Health endpoint: %s/health\n' "$MEDIA_MIXER_URL"
