#!/usr/bin/env bash
#
# Publish a test participant video stream into the MosaicComposer participant
# room. The participant stays connected until the optional timeout expires or
# the script is interrupted with Ctrl+C.
#
# Usage:
#   ./ci/mosaic/run_participant.sh [timeout-seconds]
#
# When executed inside a Docker container, the media pipeline runs directly.
# Outside Docker, it runs in the pinned runtime image.
#
# Environment overrides:
#   MOSAIC_PARTICIPANT_IMAGE     Runtime image with livekitwebrtcsink
#   MOSAIC_PARTICIPANT_IDENTITY  LiveKit participant identity
#   MOSAIC_PARTICIPANT_PATTERN   videotestsrc pattern (default: smpte)

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_PARTICIPANT_IMAGE="livekit/gstreamer:1.26.11-prod-rs"
readonly DEFAULT_PARTICIPANT_PATTERN="smpte"

usage() {
  printf 'Usage: %s [timeout-seconds]\n' "${BASH_SOURCE[0]##*/}" >&2
  printf 'Runs until interrupted when no timeout is provided.\n' >&2
}

timeout_seconds=""
case "$#" in
  0)
    ;;
  1)
    if [[ ! "$1" =~ ^[0-9]+$ ]]; then
      err "Timeout must be a non-negative integer number of seconds: $1"
      usage
      exit 2
    fi
    timeout_seconds="$1"
    ;;
  *)
    usage
    exit 2
    ;;
esac

if [[ -z "${LIVEKIT_URL:-}" || -z "${LIVEKIT_API_KEY:-}" || -z "${LIVEKIT_API_SECRET:-}" || -z "${MOSAIC_ROOM:-}" ]]; then
  err "LIVEKIT_URL, LIVEKIT_API_KEY, LIVEKIT_API_SECRET, and MOSAIC_ROOM must be set."
  exit 1
fi

participant_identity="${MOSAIC_PARTICIPANT_IDENTITY:-participant-$$}"
pattern="${MOSAIC_PARTICIPANT_PATTERN:-$DEFAULT_PARTICIPANT_PATTERN}"

inside_container=0
if [[ -f /.dockerenv ]]; then
  inside_container=1
fi

container_name=""
media_pid=""
timer_pid=""

# shellcheck disable=SC2317 # invoked by the EXIT trap
cleanup() {
  if [[ -n "$timer_pid" ]]; then
    kill "$timer_pid" 2>/dev/null || true
    wait "$timer_pid" 2>/dev/null || true
    timer_pid=""
  fi
  if [[ -n "$container_name" ]]; then
    docker rm --force "$container_name" >/dev/null 2>&1 || true
  fi
  if [[ -n "$media_pid" ]]; then
    kill -TERM "$media_pid" 2>/dev/null || true
    wait "$media_pid" 2>/dev/null || true
    media_pid=""
  fi
}

trap cleanup EXIT
trap 'exit 0' INT TERM

info "Joining room ${MOSAIC_ROOM} at ${LIVEKIT_URL} as ${participant_identity}"
info "The participant will remain connected until stopped."

gst_args=(
  videotestsrc is-live=true pattern="$pattern" '!'
  'video/x-raw,width=320,height=240,framerate=30/1' '!'
  videoconvert '!'
  vp8enc deadline=1 keyframe-max-dist=30 target-bitrate=500000 '!'
  queue '!'
  livekitwebrtcsink
    signaller::ws-url="$LIVEKIT_URL"
    signaller::api-key="$LIVEKIT_API_KEY"
    signaller::secret-key="$LIVEKIT_API_SECRET"
    signaller::room-name="$MOSAIC_ROOM"
    signaller::identity="$participant_identity"
    signaller::participant-name="$participant_identity"
    video-caps=video/x-vp8
    'stun-server=""'
)

if [[ "$inside_container" -eq 1 ]]; then
  require_command gst-launch-1.0
  require_command gst-inspect-1.0
  if ! gst-inspect-1.0 livekitwebrtcsink >/dev/null 2>&1; then
    err "The GStreamer livekitwebrtcsink plugin is not available."
    exit 1
  fi
  media_command=(gst-launch-1.0 "${gst_args[@]}")
else
  require_docker || exit 1
  participant_image="${MOSAIC_PARTICIPANT_IMAGE:-$DEFAULT_PARTICIPANT_IMAGE}"
  if ! docker_image_exists "$participant_image"; then
    err "Participant image is not available locally: $participant_image"
    err "Pull it first with: docker pull $participant_image"
    exit 1
  fi
  container_name="crowdstage-participant-$$"
  media_command=(
    docker run --rm
    --init
    --name "$container_name"
    --network host
    --entrypoint gst-launch-1.0
    "$participant_image"
    "${gst_args[@]}"
  )
fi

"${media_command[@]}" &
media_pid=$!

if [[ -n "$timeout_seconds" ]]; then
  (
    sleep "$timeout_seconds"
    kill -TERM "$media_pid" 2>/dev/null || true
  ) &
  timer_pid=$!
fi

status=0
wait "$media_pid" || status=$?
media_pid=""

if [[ -n "$timer_pid" ]]; then
  kill "$timer_pid" 2>/dev/null || true
  wait "$timer_pid" 2>/dev/null || true
  timer_pid=""
fi

if [[ -n "$timeout_seconds" ]]; then
  info "Participant ${participant_identity} stopped after ${timeout_seconds}s."
  exit 0
fi

exit "$status"
