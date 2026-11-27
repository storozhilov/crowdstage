#!/usr/bin/env bash
#
# Publish a looped test audio stream into the MediaMixer source LiveKit room
# (MEDIA_MIXER_SOURCE_ROOM). The publisher stays connected until the optional
# timeout expires or the script is interrupted with Ctrl+C.
#
# Usage:
#   ./ci/media-mixer/run_audio.sh [-n|--name 'Audio 1'] [timeout-seconds]
#
# The participant identity defaults to audio-<pid>, and the display name
# defaults to the identity. Each run picks a random audiotestsrc wave and
# frequency so concurrent audio publishers are easy to tell apart.
#
# When executed inside a Docker container, the media pipeline runs directly.
# Outside Docker, it runs in the pinned runtime image.
#
# Environment overrides:
#   MEDIA_MIXER_AUDIO_IMAGE      Runtime image with livekitwebrtcsink
#   MEDIA_MIXER_AUDIO_IDENTITY   LiveKit participant identity
#   MEDIA_MIXER_AUDIO_WAVE       audiotestsrc wave (default: random)
#   MEDIA_MIXER_AUDIO_FREQUENCY  audiotestsrc frequency in Hz (default: random)
#   MEDIA_MIXER_AUDIO_VOLUME     audiotestsrc volume from 0 to 1 (default: 0.25)

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

readonly DEFAULT_AUDIO_IMAGE="livekit/gstreamer:1.26.11-prod-rs"

readonly -a SUPPORTED_AUDIO_WAVES=(
  sine
  square
  saw
  triangle
  white-noise
  pink-noise
  sine-table
  ticks
  gaussian-noise
  red-noise
  blue-noise
  violet-noise
)

readonly -a DEFAULT_AUDIO_FREQUENCIES=(
  220
  330
  440
  554
  660
  880
)

usage() {
  printf "Usage: %s [-n|--name 'Audio 1'] [timeout-seconds]\n" "${BASH_SOURCE[0]##*/}" >&2
  printf 'Runs until interrupted when no timeout is provided.\n' >&2
}

audio_wave_is_supported() {
  local candidate="$1"
  local supported

  for supported in "${SUPPORTED_AUDIO_WAVES[@]}"; do
    if [[ "$candidate" == "$supported" ]]; then
      return 0
    fi
  done
  return 1
}

timeout_seconds=""
audio_name=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -n|--name)
      if [[ "$#" -lt 2 ]]; then
        err "Option $1 requires a name."
        usage
        exit 2
      fi
      audio_name="$2"
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

if [[ -z "${LIVEKIT_URL:-}" || -z "${LIVEKIT_API_KEY:-}" || -z "${LIVEKIT_API_SECRET:-}" || -z "${MEDIA_MIXER_SOURCE_ROOM:-}" ]]; then
  err "LIVEKIT_URL, LIVEKIT_API_KEY, LIVEKIT_API_SECRET, and MEDIA_MIXER_SOURCE_ROOM must be set."
  exit 1
fi

identity="${MEDIA_MIXER_AUDIO_IDENTITY:-audio-$$}"
name="${audio_name:-$identity}"

wave="${MEDIA_MIXER_AUDIO_WAVE:-}"
if [[ -z "$wave" ]]; then
  wave="${SUPPORTED_AUDIO_WAVES[RANDOM % ${#SUPPORTED_AUDIO_WAVES[@]}]}"
elif ! audio_wave_is_supported "$wave"; then
  err "Unsupported audiotestsrc wave: $wave"
  err "Supported waves: ${SUPPORTED_AUDIO_WAVES[*]}"
  exit 2
fi

frequency="${MEDIA_MIXER_AUDIO_FREQUENCY:-}"
if [[ -z "$frequency" ]]; then
  frequency="${DEFAULT_AUDIO_FREQUENCIES[RANDOM % ${#DEFAULT_AUDIO_FREQUENCIES[@]}]}"
elif [[ ! "$frequency" =~ ^[0-9]+$ ]]; then
  err "Invalid audio frequency: $frequency (expected an integer number of Hz)"
  exit 2
else
  frequency=$((10#$frequency))
  if (( frequency < 1 || frequency > 20000 )); then
    err "Invalid audio frequency: $frequency (expected between 1 and 20000 Hz)"
    exit 2
  fi
fi

volume="${MEDIA_MIXER_AUDIO_VOLUME:-0.25}"
if [[ ! "$volume" =~ ^(0(\.[0-9]+)?|1(\.0+)?)$ ]]; then
  err "Invalid audio volume: $volume (expected a number between 0 and 1)"
  exit 2
fi

room="${MEDIA_MIXER_SOURCE_ROOM}"

inside_container=0
if [[ -f /.dockerenv ]]; then
  inside_container=1
fi

container_name=""
media_pid=""
timer_pid=""
timeout_dir=""
timeout_marker=""

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
  if [[ -n "$timeout_dir" ]]; then
    rm -rf "$timeout_dir"
    timeout_dir=""
  fi
}

trap cleanup EXIT
trap 'exit 0' INT TERM

if [[ -n "$timeout_seconds" ]]; then
  # Use a private directory so the marker cannot be replaced by another user
  # through a predictable path in the shared temporary directory.
  timeout_dir="$(mktemp -d "${TMPDIR:-/tmp}/crowdstage-audio-timeout.XXXXXX")"
  timeout_marker="$timeout_dir/timed-out"
fi

info "Publishing ${wave} audio at ${frequency} Hz to room ${room} at ${LIVEKIT_URL} as ${identity} (${name})"
info "The audio publisher will remain connected until stopped."

gst_args=(
  audiotestsrc is-live=true wave="$wave" freq="$frequency" volume="$volume" '!'
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
    signaller::identity="$identity"
    signaller::participant-name="$name"
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
  audio_image="${MEDIA_MIXER_AUDIO_IMAGE:-$DEFAULT_AUDIO_IMAGE}"
  if ! docker_image_exists "$audio_image"; then
    err "Audio publisher image is not available locally: $audio_image"
    err "Pull it first with: docker pull $audio_image"
    exit 1
  fi
  container_name="crowdstage-audio-publisher-$$"
  media_command=(
    docker run --rm
    --init
    --name "$container_name"
    --network host
    --entrypoint gst-launch-1.0
    "$audio_image"
    "${gst_args[@]}"
  )
fi

"${media_command[@]}" &
media_pid=$!

if [[ -n "$timeout_seconds" ]]; then
  # The timer runs in a subshell, so use a marker file to tell the parent
  # whether the media process was stopped by the timeout or exited on its own.
  (
    sleep "$timeout_seconds"
    if kill -0 "$media_pid" 2>/dev/null; then
      : >"$timeout_marker"
      stop_media_process "$media_pid"
    fi
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

if [[ "$timed_out" -eq 1 && "$status" -eq 0 ]]; then
  info "Audio publisher ${identity} stopped after ${timeout_seconds}s."
  exit 0
fi

exit "$status"
