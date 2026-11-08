#!/usr/bin/env bash
#
# Stop the MosaicComposer media pipeline via the POST /stop REST endpoint.
#
# The curl request runs inside a container spawned from the builder image, so
# no HTTP client is required on the host.
#
# Usage: ./ci/mosaic/stop.sh [MOSAIC_URL]
#
# MOSAIC_URL is read from ci/.env and can be overridden with the MOSAIC_URL
# environment variable or the first argument.

set -Eeuo pipefail

# shellcheck source=../lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

MOSAIC_URL="$(mosaic_url "${1:-}")"
readonly MOSAIC_URL

require_command curl

curl -fsS -X POST "$MOSAIC_URL/stop"
