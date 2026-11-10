#!/usr/bin/env bash
#
# Stop the MediaMixer pipeline via the POST /stop REST endpoint.
#
# Usage: ./ci/media-mixer/stop.sh [MEDIA_MIXER_URL]
#
# MEDIA_MIXER_URL is read from ci/.env and can be overridden with the
# MEDIA_MIXER_URL environment variable or the first argument.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

MEDIA_MIXER_URL="$(media_mixer_url "${1:-}")"
readonly MEDIA_MIXER_URL

require_command curl

curl -fsS -X POST "$MEDIA_MIXER_URL/stop"
