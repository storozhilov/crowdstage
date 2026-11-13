#!/usr/bin/env bash
#
# Uninstall the BusMonitor instance that monitors the broadcast room.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../../lib.sh"

exec "$BUS_MONITOR_DIR/uninstall.sh" broadcast
