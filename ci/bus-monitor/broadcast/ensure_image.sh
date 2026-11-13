#!/usr/bin/env bash
#
# Ensure the BusMonitor image used by the broadcast instance exists.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../../lib.sh"

exec "$BUS_MONITOR_DIR/ensure_image.sh" "$@"
