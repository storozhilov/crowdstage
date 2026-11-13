#!/usr/bin/env bash
#
# Start the MosaicComposer media pipeline via the POST /start REST endpoint.
#
# The curl request runs inside a container spawned from the builder image, so
# no HTTP client is required on the host.
#
# Usage: ./ci/mosaic/start.sh [GRID] [--rotation-timeout SECONDS] [MOSAIC_URL]
#
# GRID optionally sets the mosaic dimensions as COLUMNSxROWS, for example 5x2
# or 5:2. Without it MosaicComposer uses its default 4x3 grid.
#
# --rotation-timeout SECONDS sets how long a guest stays in a mosaic tile
# before it is rotated with a waiting guest. Without it MosaicComposer
# uses its default 10 seconds.
#
# MOSAIC_URL is read from ci/.env and can be overridden with the MOSAIC_URL
# environment variable or a non-grid argument.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

grid=""
columns=""
rows=""
rotation_timeout=""
url=""

usage() {
  printf 'Usage: %s [GRID] [--rotation-timeout SECONDS] [MOSAIC_URL]\n' "${BASH_SOURCE[0]##*/}" >&2
}

set_rotation_timeout() {
  local value="$1"
  if [[ -n "$rotation_timeout" ]]; then
    err "rotation timeout specified more than once: $rotation_timeout and $value"
    exit 1
  fi
  if [[ ! "$value" =~ ^[0-9]+$ ]]; then
    err "Invalid rotation timeout: $value (expected a positive integer)"
    exit 1
  fi
  rotation_timeout=$((10#$value))
  if (( rotation_timeout <= 0 )); then
    err "Invalid rotation timeout: $value (expected a positive integer)"
    exit 1
  fi
}

while (( $# > 0 )); do
  arg="$1"
  shift

  case "$arg" in
    --rotation-timeout)
      if (( $# == 0 )); then
        err "Missing rotation timeout value"
        usage
        exit 1
      fi
      set_rotation_timeout "$1"
      shift
      ;;
    *)
      if [[ "$arg" =~ ^([0-9]+)[xX:]([0-9]+)$ ]]; then
        if [[ -n "$grid" ]]; then
          err "Grid dimensions specified more than once: $grid and $arg"
          exit 1
        fi
        columns=$((10#${BASH_REMATCH[1]}))
        rows=$((10#${BASH_REMATCH[2]}))
        if (( columns <= 0 || rows <= 0 )); then
          err "Grid dimensions must be positive: $arg"
          exit 1
        fi
        grid="$arg"
      elif [[ "$arg" =~ ^[0-9]+[xX:]?[0-9]*$ ]]; then
        err "Invalid grid dimensions: $arg (expected COLUMNSxROWS, e.g. 5x2)"
        exit 1
      elif [[ -n "$url" ]]; then
        err "Unexpected argument: $arg"
        usage
        exit 1
      else
        url="$arg"
      fi
      ;;
  esac
done

MOSAIC_URL="$(mosaic_url "$url")"
readonly MOSAIC_URL

require_command curl

body=""
if [[ -n "$grid" ]]; then
  body="\"columns\":$columns,\"rows\":$rows"
fi
if [[ -n "$rotation_timeout" ]]; then
  if [[ -n "$body" ]]; then
    body+=","
  fi
  body+="\"rotation_timeout_seconds\":$rotation_timeout"
fi

if [[ -n "$body" ]]; then
  curl -fsS -X POST \
    -H 'Content-Type: application/json' \
    --data "{$body}" \
    "$MOSAIC_URL/start"
else
  curl -fsS -X POST "$MOSAIC_URL/start"
fi
