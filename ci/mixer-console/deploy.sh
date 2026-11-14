#!/usr/bin/env bash
#
# Build (if needed) and deploy MixerConsole to the development stand.
#
# The script starts the LiveKit service of the stand when it is not running.
# MosaicComposer and MediaMixer are not hard dependencies: the console reports
# them as unreachable until they are deployed.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly SERVICE="mixer-console"

require_docker_compose || exit 1
ensure_env_file

"$MIXER_CONSOLE_DIR/ensure_image.sh" >/dev/null

compose --profile mixer-console up --detach --wait "$SERVICE"

printf 'MixerConsole is deployed: %s/\n' "$MIXER_CONSOLE_URL"
