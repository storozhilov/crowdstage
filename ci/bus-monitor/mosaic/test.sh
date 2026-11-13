#!/usr/bin/env bash
#
# Run the BusMonitor tests against the mosaic instance.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../../lib.sh"

exec "$BUS_MONITOR_DIR/test.sh" mosaic
