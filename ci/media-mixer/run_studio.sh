#!/usr/bin/env bash
#
# Publish test video and audio streams into the studio LiveKit room. The
# publishers stay connected until the optional timeout expires or the script is
# interrupted with Ctrl+C.
#
# Usage:
#   ./ci/media-mixer/run_studio.sh [timeout-seconds]
#
# The script publishes two participants derived from the same base identity: one
# video participant and one audio participant. When executed inside a Docker
# container, the media pipeline runs directly. Outside Docker, it runs in the
# pinned runtime image.
#
# Environment overrides:
#   MEDIA_MIXER_STUDIO_IMAGE     Runtime image with livekitwebrtcsink
#   MEDIA_MIXER_STUDIO_IDENTITY  Base LiveKit participant identity
#   MEDIA_MIXER_STUDIO_PATTERN   videotestsrc pattern (default: smpte)

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_STUDIO_IMAGE="livekit/gstreamer:1.26.11-prod-rs"
readonly DEFAULT_STUDIO_PATTERN="smpte"

usage() {
  printf 'Usage: %s [timeout-seconds]\n' "${BASH_SOURCE[0]##*/}" >&2
  printf 'Runs until interrupted when no timeout is provided.\n' >&2
}

timeout_seconds=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
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

if [[ -z "${LIVEKIT_URL:-}" || -z "${LIVEKIT_API_KEY:-}" || -z "${LIVEKIT_API_SECRET:-}" || -z "${MEDIA_MIXER_SOURCE_ROOM:-}" ]]; then
  err "LIVEKIT_URL, LIVEKIT_API_KEY, LIVEKIT_API_SECRET, and MEDIA_MIXER_SOURCE_ROOM must be set."
  exit 1
fi

identity_base="${MEDIA_MIXER_STUDIO_IDENTITY:-studio-publisher-$$}"
video_identity="${identity_base}-video"
audio_identity="${identity_base}-audio"
pattern="${MEDIA_MIXER_STUDIO_PATTERN:-$DEFAULT_STUDIO_PATTERN}"
room="${MEDIA_MIXER_SOURCE_ROOM}"

inside_container=0
if [[ -f /.dockerenv ]]; then
  inside_container=1
fi

container_name=""
media_pid=""
timer_pid=""
timeout_marker=""

# Ask the media process to stop gracefully. SIGINT lets GStreamer close its
# WebRTC sessions cleanly, so LiveKit and the studio notice the disconnect
# immediately instead of waiting for a peer timeout. Escalate if it does not
# exit within a bounded time.
stop_media_process() {
  local pid="$1"
  local deadline

  [[ -n "$pid" ]] || return 0
  if ! kill -0 "$pid" 2>/dev/null; then
    wait "$pid" 2>/dev/null || true
    return 0
  fi

  kill -INT "$pid" 2>/dev/null || true
  deadline=$((SECONDS + 5))
  while kill -0 "$pid" 2>/dev/null && (( SECONDS < deadline )); do
    sleep 0.1
  done

  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    deadline=$((SECONDS + 5))
    while kill -0 "$pid" 2>/dev/null && (( SECONDS < deadline )); do
      sleep 0.1
    done
  fi

  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null || true
  fi

  wait "$pid" 2>/dev/null || true
}

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
  if [[ -n "$timeout_marker" ]]; then
    rm -f "$timeout_marker"
  fi
}

trap cleanup EXIT
trap 'exit 0' INT TERM

info "Publishing studio test media to room ${room} at ${LIVEKIT_URL} as ${video_identity} and ${audio_identity}"
info "The publishers will remain connected until stopped."

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
    signaller::room-name="$room"
    signaller::identity="$video_identity"
    signaller::participant-name="$video_identity"
    video-caps=video/x-vp8
    'stun-server=""'

  audiotestsrc is-live=true wave=sine '!'
  'audio/x-raw,rate=48000,channels=1' '!'
  audioconvert '!'
  audioresample '!'
  opusenc '!'
  queue '!'
  livekitwebrtcsink
    signaller::ws-url="$LIVEKIT_URL"
    signaller::api-key="$LIVEKIT_API_KEY"
    signaller::secret-key="$LIVEKIT_API_SECRET"
    signaller::room-name="$room"
    signaller::identity="$audio_identity"
    signaller::participant-name="$audio_identity"
    audio-caps=audio/x-opus
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
  studio_image="${MEDIA_MIXER_STUDIO_IMAGE:-$DEFAULT_STUDIO_IMAGE}"
  if ! docker_image_exists "$studio_image"; then
    err "Studio publisher image is not available locally: $studio_image"
    err "Pull it first with: docker pull $studio_image"
    exit 1
  fi
  container_name="crowdstage-studio-publisher-$$"
  media_command=(
    docker run --rm
    --init
    --name "$container_name"
    --network host
    --entrypoint gst-launch-1.0
    "$studio_image"
    "${gst_args[@]}"
  )
fi

"${media_command[@]}" &
media_pid=$!

if [[ -n "$timeout_seconds" ]]; then
  # The timer runs in a subshell, so use a marker file to tell the parent
  # whether the media process was stopped by the timeout or exited on its own.
  timeout_marker="${TMPDIR:-/tmp}/crowdstage-studio-timeout-$$"
  rm -f "$timeout_marker"
  (
    sleep "$timeout_seconds"
    : >"$timeout_marker"
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

timed_out=0
if [[ -n "$timeout_marker" && -f "$timeout_marker" ]]; then
  timed_out=1
fi
if [[ -n "$timeout_marker" ]]; then
  rm -f "$timeout_marker"
  timeout_marker=""
fi

if [[ "$timed_out" -eq 1 ]]; then
  info "Studio publishers ${video_identity} and ${audio_identity} stopped after ${timeout_seconds}s."
  exit 0
fi

exit "$status"
