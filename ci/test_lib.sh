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
  "default ports provide URL defaults"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s|%s|%s" "$BUS_MONITOR_MOSAIC_URL" "$BUS_MONITOR_BROADCAST_URL" "$BUS_MONITOR_LIVEKIT_URL"')"
assert_eq "http://127.0.0.1:7990|http://127.0.0.1:7992|ws://127.0.0.1:7880" "$out" \
  "default ports provide BusMonitor URL defaults"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MEDIA_MIXER_URL"')"
assert_eq "http://127.0.0.1:7981" "$out" \
  "default ports provide MediaMixer URL defaults"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MIXER_CONSOLE_URL"')"
assert_eq "http://127.0.0.1:7991" "$out" \
  "default ports provide MixerConsole URL defaults"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s|%s|%s|%s|%s" "$MIXER_CONSOLE_LIVEKIT_URL" "$MIXER_CONSOLE_BROADCAST_ROOM" "$MIXER_CONSOLE_GUESTS_ROOM" "$MIXER_CONSOLE_STUDIO_ROOM" "$MIXER_CONSOLE_IDENTITY"')"
assert_eq "ws://127.0.0.1:7880|broadcast|mosaic|studio|mixer-console" "$out" \
  "MixerConsole LiveKit defaults follow the stand configuration"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MIXER_CONSOLE_LIVEKIT_URL"' \
  LIVEKIT_PORT=9600)"
assert_eq "ws://127.0.0.1:9600" "$out" \
  "environment LIVEKIT_PORT override updates MIXER_CONSOLE_LIVEKIT_URL"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MIXER_CONSOLE_BROADCAST_ROOM"' \
  MEDIA_MIXER_TARGET_ROOM=program)"
assert_eq "program" "$out" \
  "MEDIA_MIXER_TARGET_ROOM provides the MixerConsole broadcast room default"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MIXER_CONSOLE_GUESTS_ROOM"' \
  MOSAIC_TARGET_ROOM=program)"
assert_eq "program" "$out" \
  "MOSAIC_TARGET_ROOM provides the MixerConsole guests room default"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MIXER_CONSOLE_STUDIO_ROOM"' \
  MEDIA_MIXER_SOURCE_ROOM=stage)"
assert_eq "stage" "$out" \
  "MEDIA_MIXER_SOURCE_ROOM provides the MixerConsole studio room default"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MIXER_CONSOLE_LIVEKIT_URL"' \
  MIXER_CONSOLE_LIVEKIT_URL=ws://override.test:9999)"
assert_eq "ws://override.test:9999" "$out" \
  "explicit MIXER_CONSOLE_LIVEKIT_URL override wins"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MOSAIC_URL"' \
  MOSAIC_PORT=9000)"
assert_eq "http://127.0.0.1:9000" "$out" \
  "environment MOSAIC_PORT override updates MOSAIC_URL"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$LIVEKIT_URL"' \
  LIVEKIT_PORT=9100)"
assert_eq "ws://127.0.0.1:9100" "$out" \
  "environment LIVEKIT_PORT override updates LIVEKIT_URL"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$BUS_MONITOR_MOSAIC_URL"' \
  BUS_MONITOR_MOSAIC_PORT=9200)"
assert_eq "http://127.0.0.1:9200" "$out" \
  "environment BUS_MONITOR_MOSAIC_PORT override updates BUS_MONITOR_MOSAIC_URL"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$BUS_MONITOR_BROADCAST_URL"' \
  BUS_MONITOR_BROADCAST_PORT=9400)"
assert_eq "http://127.0.0.1:9400" "$out" \
  "environment BUS_MONITOR_BROADCAST_PORT override updates BUS_MONITOR_BROADCAST_URL"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MEDIA_MIXER_URL"' \
  MEDIA_MIXER_PORT=9300)"
assert_eq "http://127.0.0.1:9300" "$out" \
  "environment MEDIA_MIXER_PORT override updates MEDIA_MIXER_URL"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MIXER_CONSOLE_URL"' \
  MIXER_CONSOLE_PORT=9500)"
assert_eq "http://127.0.0.1:9500" "$out" \
  "environment MIXER_CONSOLE_PORT override updates MIXER_CONSOLE_URL"

cat >"$TEST_CI_DIR/.env" <<'EOF'
LIVEKIT_PORT=1111
MOSAIC_PORT=2222
EOF

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s|%s" "$LIVEKIT_URL" "$MOSAIC_URL"')"
assert_eq "ws://127.0.0.1:1111|http://127.0.0.1:2222" "$out" \
  ".env port overrides update URL defaults"

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MOSAIC_URL"' \
  MOSAIC_URL=http://override.test:9999)"
assert_eq "http://override.test:9999" "$out" \
  "explicit environment URL override wins"

cat >>"$TEST_CI_DIR/.env" <<'EOF'
MOSAIC_URL=http://custom.test:7777
EOF

# shellcheck disable=SC2016 # evaluated inside run_snippet
out="$(run_snippet 'source ci/lib.sh; printf "%s" "$MOSAIC_URL"')"
assert_eq "http://custom.test:7777" "$out" \
  "explicit .env URL override wins"

out="$(run_snippet 'source ci/lib.sh; env | grep "^MOSAIC_URL="')"
assert_eq "MOSAIC_URL=http://custom.test:7777" "$out" \
  "configuration variables are exported"

out="$(run_snippet 'source ci/lib.sh; force_requested')"
assert_eq "0" "$out" "force_requested defaults to 0"

out="$(run_snippet 'source ci/lib.sh; force_requested --force')"
assert_eq "1" "$out" "force_requested accepts --force"

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$failed"
