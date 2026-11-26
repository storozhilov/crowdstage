#!/usr/bin/env bash
#
# Shared helpers for the CrowdStage CI scripts.
#
# Source this file after "set -Eeuo pipefail". It defines the common project
# paths, loads configuration from ci/.env, and provides the Docker checks,
# environment-file helpers, and the Compose wrapper used by the scripts.

if (( BASH_VERSINFO[0] < 4 )); then
  echo "This script requires Bash 4+; found ${BASH_VERSION}" >&2
  exit 1
fi

CI_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
readonly CI_DIR
REPO_ROOT="$(cd -- "$CI_DIR/.." >/dev/null 2>&1 && pwd -P)"
readonly REPO_ROOT
readonly COMPOSE_DIR="$CI_DIR/compose"
readonly COMPOSE_FILE="$COMPOSE_DIR/docker-compose.yml"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly MOSAIC_DIR="$CI_DIR/mosaic"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly MEDIA_MIXER_DIR="$CI_DIR/media-mixer"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly CONTROL_PLANE_DIR="$CI_DIR/control-plane"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly BUS_MONITOR_DIR="$CI_DIR/bus-monitor"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly MIXER_CONSOLE_DIR="$CI_DIR/mixer-console"
readonly ENV_FILE="$CI_DIR/.env"
readonly ENV_EXAMPLE="$CI_DIR/.env.example"
readonly PROJECT_NAME="event-platform-dev"

# shellcheck disable=SC2034 # used by the scripts that source this library
readonly BUILD_DIR="$REPO_ROOT/build"
readonly CACHE_DIR="$REPO_ROOT/.cache"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly MODULE_CACHE_DIR="$CACHE_DIR/go-mod"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly BUILD_CACHE_DIR="$CACHE_DIR/go-build"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly E2E_BUILD_CACHE_DIR="$CACHE_DIR/go-build-e2e"
# shellcheck disable=SC2034 # used by the scripts that source this library
readonly NPM_CACHE_DIR="$CACHE_DIR/npm"

USER_ID="$(id -u)"
readonly USER_ID
GROUP_ID="$(id -g)"
readonly GROUP_ID

info() {
  printf '%s\n' "$*"
}

err() {
  printf 'Error: %s\n' "$*" >&2
}

on_error() {
  local exit_code=$?
  local source="${BASH_SOURCE[1]:-${BASH_SOURCE[0]}}"
  printf 'Error: %s failed at line %s (exit code %s)\n' \
    "${source##*/}" "$1" "$exit_code" >&2
}

ensure_env_file() {
  if [[ -f "$ENV_FILE" ]]; then
    return 0
  fi

  if [[ ! -f "$ENV_EXAMPLE" ]]; then
    err "Missing environment file: $ENV_FILE"
    err "Also missing example file: $ENV_EXAMPLE"
    return 1
  fi

  cp -- "$ENV_EXAMPLE" "$ENV_FILE"
  info "Created $ENV_FILE from .env.example"
}

# Load ci/.env.example and ci/.env, then restore any variables that were already
# set in the environment so explicit overrides take precedence. The example file
# provides defaults, while ci/.env is the local override file.
load_env() {
  local -a files=()
  [[ -f "$ENV_EXAMPLE" ]] && files+=("$ENV_EXAMPLE")
  [[ -f "$ENV_FILE" ]] && files+=("$ENV_FILE")

  if (( ${#files[@]} == 0 )); then
    return 1
  fi

  local -A original=()
  local file line name
  for file in "${files[@]}"; do
    while IFS= read -r line || [[ -n "$line" ]]; do
      if [[ "$line" =~ ^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)= ]]; then
        name="${BASH_REMATCH[1]}"
        if [[ -z "${original[$name]+set}" && -n "${!name+x}" ]]; then
          original["$name"]="${!name}"
        fi
      fi
    done < "$file"
  done

  set -a
  for file in "${files[@]}"; do
    # shellcheck disable=SC1090
    source "$file"
  done
  set +a

  for name in "${!original[@]}"; do
    export "$name=${original[$name]}"
  done
}

require_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    err "Docker is not installed or is not available in PATH."
    printf '%s\n' \
      'Install Docker Engine and Docker Compose V2, then try again:' \
      '  https://docs.docker.com/engine/install/' >&2
    return 1
  fi

  if ! docker info >/dev/null 2>&1; then
    err "The Docker daemon is not reachable."
    printf '%s\n' \
      'Make sure Docker is running and that your user can access the daemon.' \
      'On Linux this usually means adding your user to the "docker" group and' \
      'starting a new login session. This script does not use sudo automatically.' >&2
    return 1
  fi
}

require_docker_compose() {
  require_docker || return 1

  if ! docker compose version >/dev/null 2>&1; then
    err "Docker Compose V2 is not available ('docker compose' failed)."
    printf '%s\n' \
      'Install the Docker Compose plugin for your Docker installation:' \
      '  https://docs.docker.com/compose/install/' >&2
    return 1
  fi
}

compose() {
  docker compose \
    --project-name "$PROJECT_NAME" \
    --project-directory "$COMPOSE_DIR" \
    --env-file "$ENV_FILE" \
    --file "$COMPOSE_FILE" \
    "$@"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    err "Required command is not available: $1"
    exit 1
  fi
}

# Print "1" when -f/--force is present, "0" otherwise. Reject unknown arguments.
force_requested() {
  local arg
  for arg in "$@"; do
    case "$arg" in
      -f|--force)
        printf '1'
        return 0
        ;;
      *)
        err "Unknown argument: $arg"
        printf 'Usage: %s [-f|--force]\n' "${BASH_SOURCE[1]##*/}" >&2
        return 1
        ;;
    esac
  done
  printf '0'
}

docker_image_exists() {
  docker image inspect "$1" >/dev/null 2>&1
}

build_docker_image() {
  local dockerfile="$1"
  local image="$2"
  local context="$3"

  docker build \
    --file "$dockerfile" \
    --tag "$image" \
    --build-arg "USER_ID=$USER_ID" \
    --build-arg "GROUP_ID=$GROUP_ID" \
    "$context" >&2
}

# Patterns supported by the pinned GStreamer videotestsrc element.
readonly -a SUPPORTED_VIDEOTEST_PATTERNS=(
  smpte
  snow
  black
  white
  red
  green
  blue
  checkers-1
  checkers-2
  checkers-4
  checkers-8
  circular
  blink
  smpte75
  zone-plate
  gamut
  chroma-zone-plate
  solid-color
  ball
  smpte100
  bar
  pinwheel
  spokes
  gradient
  colors
  smpte-rp-219
)

# Patterns that produce visibly different moving test videos. A pattern is
# picked at random for each publisher so concurrent publishers are easy to
# tell apart.
readonly -a DEFAULT_VIDEOTEST_PATTERNS=(
  smpte
  smpte75
  smpte100
  colors
  gradient
  checkers-1
  checkers-2
  checkers-4
  snow
  ball
  bar
  pinwheel
  zone-plate
  chroma-zone-plate
  gamut
)

videotest_pattern_is_supported() {
  local candidate="$1"
  local supported

  for supported in "${SUPPORTED_VIDEOTEST_PATTERNS[@]}"; do
    if [[ "$candidate" == "$supported" ]]; then
      return 0
    fi
  done
  return 1
}

# Assign the requested videotestsrc pattern to the variable named by the first
# argument after validating it, or a random pattern from the defaults when no
# override is provided.
select_videotest_pattern() {
  local target_name="$1"
  local requested_pattern="$2"

  if [[ -z "$requested_pattern" ]]; then
    printf -v "$target_name" '%s' \
      "${DEFAULT_VIDEOTEST_PATTERNS[RANDOM % ${#DEFAULT_VIDEOTEST_PATTERNS[@]}]}"
    return 0
  fi

  if ! videotest_pattern_is_supported "$requested_pattern"; then
    err "Unsupported videotestsrc pattern: $requested_pattern"
    err "Supported patterns: ${SUPPORTED_VIDEOTEST_PATTERNS[*]}"
    return 1
  fi

  printf -v "$target_name" '%s' "$requested_pattern"
}

# Ask a media process to stop gracefully. SIGINT lets GStreamer close its
# WebRTC sessions cleanly, so LiveKit and remote peers notice the disconnect
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

mosaic_url() {
  local url="${1:-${MOSAIC_URL:-}}"
  if [[ -z "$url" ]]; then
    err "MOSAIC_URL is not set and no URL was provided."
    return 1
  fi
  printf '%s\n' "$url"
}

control_plane_url() {
  local url="${1:-${CONTROL_PLANE_URL:-}}"
  if [[ -z "$url" ]]; then
    err "CONTROL_PLANE_URL is not set and no URL was provided."
    return 1
  fi
  printf '%s\n' "$url"
}

media_mixer_url() {
  local url="${1:-${MEDIA_MIXER_URL:-}}"
  if [[ -z "$url" ]]; then
    err "MEDIA_MIXER_URL is not set and no URL was provided."
    return 1
  fi
  printf '%s\n' "$url"
}

load_env || true

# Derive host URLs after loading and applying overrides so that changing only a
# port updates the corresponding URL. Explicit URL values still take precedence.
LIVEKIT_URL="${LIVEKIT_URL:-ws://127.0.0.1:${LIVEKIT_PORT}}"
MOSAIC_URL="${MOSAIC_URL:-http://127.0.0.1:${MOSAIC_PORT}}"
MEDIA_MIXER_URL="${MEDIA_MIXER_URL:-http://127.0.0.1:${MEDIA_MIXER_PORT}}"
CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://127.0.0.1:${CONTROL_PLANE_PORT:-7982}}"
CONTROL_PLANE_LIVEKIT_URL="${CONTROL_PLANE_LIVEKIT_URL:-${LIVEKIT_URL}}"
MIXER_CONSOLE_CONTROL_PLANE_URL="${MIXER_CONSOLE_CONTROL_PLANE_URL:-${CONTROL_PLANE_URL}}"
BUS_MONITOR_CONTROL_PLANE_URL="${BUS_MONITOR_CONTROL_PLANE_URL:-${CONTROL_PLANE_URL}}"
BUS_MONITOR_MOSAIC_URL="${BUS_MONITOR_MOSAIC_URL:-http://127.0.0.1:${BUS_MONITOR_MOSAIC_PORT}}"
BUS_MONITOR_BROADCAST_URL="${BUS_MONITOR_BROADCAST_URL:-http://127.0.0.1:${BUS_MONITOR_BROADCAST_PORT}}"
CONTROL_PLANE_BROADCAST_ROOM="${CONTROL_PLANE_BROADCAST_ROOM:-${MEDIA_MIXER_TARGET_ROOM:-broadcast}}"
CONTROL_PLANE_GUESTS_ROOM="${CONTROL_PLANE_GUESTS_ROOM:-${MOSAIC_TARGET_ROOM:-mosaic}}"
CONTROL_PLANE_GUESTS_SOURCE_ROOM="${CONTROL_PLANE_GUESTS_SOURCE_ROOM:-${MOSAIC_SOURCE_ROOM:-guests}}"
CONTROL_PLANE_STUDIO_ROOM="${CONTROL_PLANE_STUDIO_ROOM:-${MEDIA_MIXER_SOURCE_ROOM:-studio}}"
CONTROL_PLANE_IDENTITY="${CONTROL_PLANE_IDENTITY:-control-plane}"
MEDIA_MIXER_GUEST_ROOM="${MEDIA_MIXER_GUEST_ROOM:-${MOSAIC_SOURCE_ROOM:-guests}}"
MEDIA_MIXER_MOSAIC_ROOM="${MEDIA_MIXER_MOSAIC_ROOM:-${MOSAIC_TARGET_ROOM:-mosaic}}"
MEDIA_MIXER_STUDIO_IMAGE="${MEDIA_MIXER_STUDIO_IMAGE:-livekit/gstreamer:1.26.11-prod-rs}"
MIXER_CONSOLE_URL="${MIXER_CONSOLE_URL:-http://127.0.0.1:${MIXER_CONSOLE_PORT:-7991}}"
normalize_origin() {
  local url="$1"
  if [[ "$url" =~ ^([A-Za-z][A-Za-z0-9+.-]*)://([^/?#]*)([/?#].*)?$ ]]; then
    local scheme="${BASH_REMATCH[1],,}"
    local authority="${BASH_REMATCH[2],,}"
    printf '%s://%s' "$scheme" "$authority"
  else
    printf '%s' "$url"
  fi
}

control_plane_default_origins() {
  local -a result=()
  local configured origin alias
  for configured in "$MIXER_CONSOLE_URL" "$BUS_MONITOR_MOSAIC_URL" "$BUS_MONITOR_BROADCAST_URL"; do
    [[ -n "$configured" ]] || continue
    origin="$(normalize_origin "$configured")"
    result+=("$origin")
    case "$origin" in
      http://127.0.0.1:*)
        alias="http://localhost:${origin#http://127.0.0.1:}"
        ;;
      http://localhost:*)
        alias="http://127.0.0.1:${origin#http://localhost:}"
        ;;
      *)
        alias=""
        ;;
    esac
    if [[ -n "$alias" ]]; then
      result+=("$alias")
    fi
  done
  local IFS=,
  printf '%s' "${result[*]}"
}

CONTROL_PLANE_ALLOWED_ORIGINS="${CONTROL_PLANE_ALLOWED_ORIGINS:-$(control_plane_default_origins)}"
export LIVEKIT_URL MOSAIC_URL MEDIA_MIXER_URL MEDIA_MIXER_GUEST_ROOM MEDIA_MIXER_MOSAIC_ROOM
export CONTROL_PLANE_URL CONTROL_PLANE_LIVEKIT_URL MIXER_CONSOLE_CONTROL_PLANE_URL BUS_MONITOR_CONTROL_PLANE_URL
export BUS_MONITOR_MOSAIC_URL BUS_MONITOR_BROADCAST_URL
export MIXER_CONSOLE_URL MEDIA_MIXER_STUDIO_IMAGE
export CONTROL_PLANE_BROADCAST_ROOM CONTROL_PLANE_GUESTS_ROOM CONTROL_PLANE_GUESTS_SOURCE_ROOM
export CONTROL_PLANE_STUDIO_ROOM CONTROL_PLANE_IDENTITY CONTROL_PLANE_ALLOWED_ORIGINS
