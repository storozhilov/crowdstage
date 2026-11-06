#!/usr/bin/env bash
#
# Ensure the generated test data exists in the project tree.
#
# The media fixtures are generated inside the builder container so that no
# media tools are required on the host. The builder image is rebuilt with
# --force when it does not provide ffmpeg yet.
#
# The resulting fixture is written under mosaic/testdata/.

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

readonly TESTDATA_DIR="$REPO_ROOT/mosaic/testdata"
readonly FORWARDING_FIXTURE="$TESTDATA_DIR/forwarding.ivf"

require_docker || exit 1

if [[ -s "$FORWARDING_FIXTURE" ]]; then
  printf 'Test data already exists: %s\n' "${FORWARDING_FIXTURE#"$REPO_ROOT"/}" >&2
  exit 0
fi

builder_image="$("$SCRIPT_DIR/ensure_builder.sh")"
if ! docker run --rm "$builder_image" sh -c 'command -v ffmpeg >/dev/null 2>&1'; then
  builder_image="$("$SCRIPT_DIR/ensure_builder.sh" --force)"
fi

mkdir -p "$TESTDATA_DIR"
temporary_fixture="$FORWARDING_FIXTURE.tmp"
trap 'rm -f "$temporary_fixture"' EXIT

docker run --rm \
  --mount "type=bind,source=$TESTDATA_DIR,target=/testdata" \
  --workdir /tmp \
  "$builder_image" \
  ffmpeg -y -hide_banner -loglevel error \
  -f lavfi -i "testsrc2=size=96x64:rate=30:duration=1" \
  -c:v libvpx -b:v 100k -g 10 -keyint_min 10 -auto-alt-ref 0 \
  -f ivf /testdata/forwarding.ivf.tmp

mv "$temporary_fixture" "$FORWARDING_FIXTURE"
trap - EXIT

printf 'Created test data: %s\n' "${FORWARDING_FIXTURE#"$REPO_ROOT"/}"
