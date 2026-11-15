#!/usr/bin/env bash
#
# Switch the broadcasted studio video stream via the POST /switch REST endpoint.
#
# Usage: ./ci/media-mixer/switch_source.sh PARTICIPANT_IDENTITY [MEDIA_MIXER_URL]
#
# MEDIA_MIXER_URL is read from ci/.env and can be overridden with the
# MEDIA_MIXER_URL environment variable or the second argument.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

if [[ "$#" -lt 1 || -z "${1:-}" ]]; then
  err "Usage: ${BASH_SOURCE[0]##*/} PARTICIPANT_IDENTITY [MEDIA_MIXER_URL]"
  exit 2
fi

readonly PARTICIPANT_IDENTITY="$1"
MEDIA_MIXER_URL="$(media_mixer_url "${2:-}")"
readonly MEDIA_MIXER_URL

require_command curl
require_command jq

payload="$(jq -nc --arg identity "$PARTICIPANT_IDENTITY" '{participant_identity: $identity}')"

curl -fsS \
  -X POST \
  -H 'Content-Type: application/json' \
  --data "$payload" \
  "$MEDIA_MIXER_URL/switch"
