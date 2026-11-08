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
# MOSAIC_URL defaults to http://127.0.0.1:7980 and can be overridden with the
# MOSAIC_URL environment variable or the first argument.

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
# shellcheck source=../lib.sh
source "$SCRIPT_DIR/../lib.sh"

readonly DEFAULT_URL="http://127.0.0.1:7980"
readonly MOSAIC_URL="${1:-${MOSAIC_URL:-$DEFAULT_URL}}"

require_command curl
require_command jq

curl -fsS "$MOSAIC_URL/health" | jq .
