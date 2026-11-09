#!/usr/bin/env bash
#
# Start the MosaicComposer media pipeline via the POST /start REST endpoint.
#
# The curl request runs inside a container spawned from the builder image, so
# no HTTP client is required on the host.
#
# Usage: ./ci/mosaic/start.sh [GRID] [MOSAIC_URL]
#
# GRID optionally sets the mosaic dimensions as COLUMNSxROWS, for example 5x2
# or 5:2. Without it MosaicComposer uses its default 4x3 grid.
#
# MOSAIC_URL is read from ci/.env and can be overridden with the MOSAIC_URL
# environment variable or a non-grid argument.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

grid=""
columns=""
rows=""
url=""

for arg in "$@"; do
  if [[ "$arg" =~ ^([0-9]+)[xX:]([0-9]+)$ ]]; then
    if [[ -n "$grid" ]]; then
      err "Grid dimensions specified more than once: $grid and $arg"
      exit 1
    fi
    columns="${BASH_REMATCH[1]}"
    rows="${BASH_REMATCH[2]}"
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
    printf 'Usage: %s [GRID] [MOSAIC_URL]\n' "${BASH_SOURCE[0]##*/}" >&2
    exit 1
  else
    url="$arg"
  fi
done

MOSAIC_URL="$(mosaic_url "$url")"
readonly MOSAIC_URL

require_command curl

if [[ -n "$grid" ]]; then
  curl -fsS -X POST \
    -H 'Content-Type: application/json' \
    --data "{\"columns\":$columns,\"rows\":$rows}" \
    "$MOSAIC_URL/start"
else
  curl -fsS -X POST "$MOSAIC_URL/start"
fi
