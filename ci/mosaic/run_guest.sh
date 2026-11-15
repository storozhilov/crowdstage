#!/usr/bin/env bash
#
# Publish a test guest video stream into the MosaicComposer source room
# (MOSAIC_SOURCE_ROOM). The guest stays connected until the optional timeout
# expires or the script is interrupted with Ctrl+C.
#
# Usage:
#   ./ci/mosaic/run_guest.sh [-n|--name 'Vasya Pupkin'] [timeout-seconds]
#
# The guest name defaults to the LiveKit participant identity.
#
# When executed inside a Docker container, the media pipeline runs directly.
# Outside Docker, it runs in the pinned runtime image.
#
# Environment overrides:
#   MOSAIC_GUEST_IMAGE     Runtime image with livekitwebrtcsink
#   MOSAIC_GUEST_IDENTITY  LiveKit participant identity
#   MOSAIC_GUEST_PATTERN   videotestsrc pattern (default: random)

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_GUEST_IMAGE="livekit/gstreamer:1.26.11-prod-rs"

usage() {
  printf "Usage: %s [-n|--name 'Vasya Pupkin'] [timeout-seconds]\n" "${BASH_SOURCE[0]##*/}" >&2
  printf 'Runs until interrupted when no timeout is provided.\n' >&2
}

timeout_seconds=""
guest_name=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -n|--name)
      if [[ "$#" -lt 2 ]]; then
        err "Option $1 requires a name."
        usage
        exit 2
      fi
      guest_name="$2"
      shift 2
      ;;
    *)
      if [[ "$1" =~ ^[0-9]+$ ]]; then
        if [[ -n "$timeout_seconds" ]]; then
          err "Timeout was provided more than once: $1"
        else
          timeout_seconds="$1"
          shift
          continue
        fi
      elif [[ "$1" =~ ^-?[0-9]+$ || "$1" != -* ]]; then
        err "Timeout must be a non-negative integer number of seconds: $1"
      else
        err "Unknown argument: $1"
      fi
      usage
      exit 2
      ;;
  esac
done

if [[ -z "${LIVEKIT_URL:-}" || -z "${LIVEKIT_API_KEY:-}" || -z "${LIVEKIT_API_SECRET:-}" || -z "${MOSAIC_SOURCE_ROOM:-}" ]]; then
  err "LIVEKIT_URL, LIVEKIT_API_KEY, LIVEKIT_API_SECRET, and MOSAIC_SOURCE_ROOM must be set."
  exit 1
fi

guest_identity="${MOSAIC_GUEST_IDENTITY:-guest-$$}"
guest_name="${guest_name:-$guest_identity}"
if ! select_videotest_pattern pattern "${MOSAIC_GUEST_PATTERN:-}"; then
  exit 2
fi

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
  if [[ -n "$media_pid" ]]; then
    stop_media_process "$media_pid"
    media_pid=""
  fi
  if [[ -n "$container_name" ]]; then
    docker rm --force "$container_name" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT
trap 'exit 0' INT TERM

info "Joining room ${MOSAIC_SOURCE_ROOM} at ${LIVEKIT_URL} as ${guest_identity} (${guest_name})"
info "The guest will remain connected until stopped."

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
    signaller::room-name="$MOSAIC_SOURCE_ROOM"
    signaller::identity="$guest_identity"
    signaller::participant-name="$guest_name"
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
  guest_image="${MOSAIC_GUEST_IMAGE:-$DEFAULT_GUEST_IMAGE}"
  if ! docker_image_exists "$guest_image"; then
    err "Guest image is not available locally: $guest_image"
    err "Pull it first with: docker pull $guest_image"
    exit 1
  fi
  container_name="crowdstage-guest-$$"
  media_command=(
    docker run --rm
    --init
    --name "$container_name"
    --network host
    --entrypoint gst-launch-1.0
    "$guest_image"
    "${gst_args[@]}"
  )
fi

"${media_command[@]}" &
media_pid=$!

if [[ -n "$timeout_seconds" ]]; then
  (
    sleep "$timeout_seconds"
    stop_media_process "$media_pid"
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
  info "Guest ${guest_identity} stopped after ${timeout_seconds}s."
  exit 0
fi

exit "$status"
