#!/usr/bin/env bash
#
# Start the MediaMixer pipeline via the POST /start REST endpoint.
#
# Usage: ./ci/media-mixer/start.sh [--guest-broadcast-seconds SECONDS] [MEDIA_MIXER_URL]
#
# --guest-broadcast-seconds SECONDS sets how long a guest stream stays on the
# broadcast output after it is selected in MixerConsole. Without it MediaMixer
# uses its default of 10 seconds.
#
# MEDIA_MIXER_URL is read from ci/.env and can be overridden with the
# MEDIA_MIXER_URL environment variable or a positional argument.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

guest_broadcast_seconds=""
url=""

usage() {
  printf 'Usage: %s [--guest-broadcast-seconds SECONDS] [MEDIA_MIXER_URL]\n' \
    "${BASH_SOURCE[0]##*/}" >&2
}

set_guest_broadcast_seconds() {
  local value="$1"
  if [[ -n "$guest_broadcast_seconds" ]]; then
    err "guest broadcast duration specified more than once: $guest_broadcast_seconds and $value"
    exit 1
  fi
  if [[ ! "$value" =~ ^[0-9]+$ ]]; then
    err "Invalid guest broadcast duration: $value (expected a positive integer)"
    exit 1
  fi
  guest_broadcast_seconds=$((10#$value))
  if (( guest_broadcast_seconds <= 0 )); then
    err "Invalid guest broadcast duration: $value (expected a positive integer)"
    exit 1
  fi
}

while (( $# > 0 )); do
  arg="$1"
  shift

  case "$arg" in
    --guest-broadcast-seconds)
      if (( $# == 0 )); then
        err "Missing guest broadcast duration value"
        usage
        exit 1
      fi
      set_guest_broadcast_seconds "$1"
      shift
      ;;
    --guest-broadcast-seconds=*)
      set_guest_broadcast_seconds "${arg#*=}"
      ;;
    -*)
      err "Unknown argument: $arg"
      usage
      exit 1
      ;;
    *)
      if [[ -n "$url" ]]; then
        err "Unexpected argument: $arg"
        usage
        exit 1
      fi
      url="$arg"
      ;;
  esac
done

MEDIA_MIXER_URL="$(media_mixer_url "$url")"
readonly MEDIA_MIXER_URL

require_command curl

if [[ -n "$guest_broadcast_seconds" ]]; then
  curl -fsS -X POST \
    -H 'Content-Type: application/json' \
    --data "{\"guest_broadcast_seconds\":$guest_broadcast_seconds}" \
    "$MEDIA_MIXER_URL/start"
else
  curl -fsS -X POST "$MEDIA_MIXER_URL/start"
fi
