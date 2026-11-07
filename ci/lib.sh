#!/usr/bin/env bash
#
# Shared helpers for the CrowdStage CI scripts.
#
# Source this file after "set -Eeuo pipefail". It defines the common project
# paths, Docker checks, environment-file helpers, and the Compose wrapper.

readonly CI_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd -P)"
readonly REPO_ROOT="$(cd -- "$CI_DIR/.." >/dev/null 2>&1 && pwd -P)"
readonly COMPOSE_DIR="$CI_DIR/compose"
readonly COMPOSE_FILE="$COMPOSE_DIR/docker-compose.yml"
readonly ENV_FILE="$CI_DIR/.env"
readonly ENV_EXAMPLE="$CI_DIR/.env.example"
readonly PROJECT_NAME="event-platform-dev"

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

load_env_file() {
  if [[ -f "$ENV_FILE" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
    return 0
  fi

  if [[ -f "$ENV_EXAMPLE" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$ENV_EXAMPLE"
    set +a
    return 0
  fi

  return 1
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
