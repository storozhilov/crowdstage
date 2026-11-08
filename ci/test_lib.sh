#!/usr/bin/env bash
#
# Tests for ci/lib.sh configuration loading and common helpers.
#
# The loader is exercised in a temporary copy of lib.sh and .env.example so the
# repository's ci/.env is not modified.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib.sh"

TEST_DIR="$(mktemp -d)"
readonly TEST_DIR
readonly TEST_CI_DIR="$TEST_DIR/ci"

cleanup() {
  # shellcheck disable=SC2317 # invoked by the EXIT trap
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$TEST_CI_DIR"
cp "$CI_DIR/lib.sh" "$CI_DIR/.env.example" "$TEST_CI_DIR/"

passed=0
failed=0

ok() {
  passed=$((passed + 1))
  printf 'ok - %s\n' "$*"
}

not_ok() {
  failed=$((failed + 1))
  printf 'not ok - %s\n' "$*" >&2
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local label="$3"

  if [[ "$actual" == "$expected" ]]; then
    ok "$label"
  else
    not_ok "$label (expected '$expected', got '$actual')"
  fi
}

run_snippet() {
  local snippet="$1"
  shift

  env -i PATH="$PATH" HOME="$HOME" TEST_CI_DIR="$TEST_CI_DIR" "$@" \
    bash -c "set -Eeuo pipefail; cd \"\$TEST_CI_DIR/..\"; $snippet"
}

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s|%s" "$LIVEKIT_URL" "$MOSAIC_URL"')"
assert_eq "ws://127.0.0.1:7880|http://127.0.0.1:7980" "$out" \
  "example file provides URL defaults"

cat >"$TEST_CI_DIR/.env" <<'EOF'
LIVEKIT_PORT=1111
MOSAIC_PORT=2222
LIVEKIT_URL=ws://127.0.0.1:${LIVEKIT_PORT}
MOSAIC_URL=http://127.0.0.1:${MOSAIC_PORT}
EOF

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s|%s" "$LIVEKIT_URL" "$MOSAIC_URL"')"
assert_eq "ws://127.0.0.1:1111|http://127.0.0.1:2222" "$out" \
  ".env overrides example defaults"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MOSAIC_URL"' \
  MOSAIC_URL=http://override.test:9999)"
assert_eq "http://override.test:9999" "$out" \
  "environment overrides .env and example"

out="$(run_snippet 'source ci/lib.sh; env | grep "^MOSAIC_URL="')"
assert_eq "MOSAIC_URL=http://127.0.0.1:2222" "$out" \
  "configuration variables are exported"

out="$(run_snippet 'source ci/lib.sh; force_requested')"
assert_eq "0" "$out" "force_requested defaults to 0"

out="$(run_snippet 'source ci/lib.sh; force_requested --force')"
assert_eq "1" "$out" "force_requested accepts --force"

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
