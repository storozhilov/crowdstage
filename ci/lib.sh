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

mosaic_url() {
  local url="${1:-${MOSAIC_URL:-}}"
  if [[ -z "$url" ]]; then
    err "MOSAIC_URL is not set and no URL was provided."
    return 1
  fi
  printf '%s\n' "$url"
}

load_env || true
