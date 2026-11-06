#!/usr/bin/env bash
#
# Run the CrowdStage Go tests inside the builder container.
#
# Any arguments are passed to "go test", for example:
#   ./ci/run_tests.sh -run TestGreeting -v
#
# The Go module cache and build cache are kept in .cache/ on the host so that
# repeated runs do not download modules or recompile unchanged packages.
#
# Tests run against the LiveKit server on the development stand. Start the stand
# first, for example:
#   ./ci/dev.sh up --wait
#   ./ci/run_tests.sh

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
readonly REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." >/dev/null 2>&1 && pwd -P)"
readonly CACHE_DIR="$REPO_ROOT/.cache"
readonly MODULE_CACHE_DIR="$CACHE_DIR/go-mod"
readonly BUILD_CACHE_DIR="$CACHE_DIR/go-build"
readonly ENV_FILE="$SCRIPT_DIR/.env"
readonly ENV_EXAMPLE="$SCRIPT_DIR/.env.example"

readonly BUILDER_IMAGE="$("$SCRIPT_DIR/ensure_builder.sh")"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
elif [[ -f "$ENV_EXAMPLE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_EXAMPLE"
  set +a
fi

mkdir -p "$MODULE_CACHE_DIR" "$BUILD_CACHE_DIR"

export LIVEKIT_URL="${LIVEKIT_URL:-ws://127.0.0.1:${LIVEKIT_PORT:-7880}}"

docker_args=(
  --rm
  --network host
  --mount "type=bind,source=$REPO_ROOT,target=/src"
  --mount "type=bind,source=$MODULE_CACHE_DIR,target=/cache/go-mod"
  --mount "type=bind,source=$BUILD_CACHE_DIR,target=/cache/go-build"
  --env GOMODCACHE=/cache/go-mod
  --env GOCACHE=/cache/go-build
  --env LIVEKIT_URL
  --env LIVEKIT_API_KEY
  --env LIVEKIT_API_SECRET
)

docker run "${docker_args[@]}" \
  --workdir /src/app \
  "$BUILDER_IMAGE" \
  go test ./... "$@"
