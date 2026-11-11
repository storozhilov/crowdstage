#!/usr/bin/env bash
#
# Print the MediaMixer health information via the GET /health REST endpoint.
#
# The JSON response is pretty-printed with jq for human-readable output.
#
# Usage: ./ci/media-mixer/get_health.sh [MEDIA_MIXER_URL]
#
# MEDIA_MIXER_URL is read from ci/.env and can be overridden with the
# MEDIA_MIXER_URL environment variable or the first argument.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

MEDIA_MIXER_URL="$(media_mixer_url "${1:-}")"
readonly MEDIA_MIXER_URL

require_command curl
require_command jq

curl -fsS "$MEDIA_MIXER_URL/health" | jq .
