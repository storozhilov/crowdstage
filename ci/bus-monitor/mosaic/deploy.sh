#!/usr/bin/env bash
#
# Deploy the BusMonitor instance that monitors the mosaic output room.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../../lib.sh"

exec "$BUS_MONITOR_DIR/deploy.sh" mosaic
