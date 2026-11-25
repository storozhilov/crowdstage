#!/usr/bin/env bash
#
# Run ControlPlane lint and unit tests inside the builder image.
#
# Usage: ./ci/control-plane/test.sh

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE

mkdir -p "$NPM_CACHE_DIR"

printf 'Running ControlPlane tests\n'
docker run \
  --rm \
  --init \
  --mount "type=bind,source=$REPO_ROOT,target=/src" \
  --mount "type=bind,source=$NPM_CACHE_DIR,target=/cache/npm" \
  --env NPM_CONFIG_CACHE=/cache/npm \
  --workdir /src/control-plane \
  "$BUILDER_IMAGE" \
  bash -c "npm ci && npm run check && npm run test:unit"
