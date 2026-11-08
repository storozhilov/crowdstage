#!/usr/bin/env bash
#
# Print the MosaicComposer health information via the GET /health REST endpoint.
#
# The curl request runs inside a container spawned from the builder image, so
# no HTTP client is required on the host. The JSON response is pretty-printed
# with jq for human-readable output.
#
# Usage: ./ci/mosaic/get_health.sh [MOSAIC_URL]
#
# MOSAIC_URL is read from ci/.env and can be overridden with the MOSAIC_URL
# environment variable or the first argument.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

MOSAIC_URL="$(mosaic_url "${1:-}")"
readonly MOSAIC_URL

require_command curl
require_command jq

curl -fsS "$MOSAIC_URL/health" | jq .
