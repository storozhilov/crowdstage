#!/usr/bin/env bash
#
# Manage the local development stand for the event platform.
#
# Usage: ./ci/dev.sh <command> [arguments]
# Run "./ci/dev.sh help" for the list of commands.

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib.sh"

trap 'on_error $LINENO' ERR

usage() {
  cat <<'EOF'
Manage the local development stand (Docker Compose project: event-platform-dev).

Usage:
  ./ci/dev.sh <command> [arguments]

Commands:
  up                  Create and start the stand in the background.
  down                Stop and remove containers and networks, keeping volumes.
  restart             Restart the stand (starts it if it does not exist yet).
  status              Show all services, including stopped ones.
  ps                  Show containers for this Compose project.
  logs [service]      Show logs for all services or for one service.
  follow [service]    Follow logs for all services or for one service.
  pull                Pull the images required by the stand.
  build               Build images defined by the Compose configuration.
  clean               Stop the stand and delete its volumes (asks for confirmation).
  help                Show this help.

Extra arguments are passed to the matching "docker compose" command, for example:
  ./ci/dev.sh up --force-recreate
  ./ci/dev.sh logs --tail=200 livekit
  ./ci/dev.sh follow livekit

The .env file is created from .env.example automatically when a command needs it.
EOF
}

cmd_up() {
  ensure_env_file
  compose up --detach "$@"
}

cmd_down() {
  local arg
  for arg in "$@"; do
    case "$arg" in
      -v|--volumes)
        err "The 'down' command preserves volumes. Use 'clean' to remove them."
        exit 1
        ;;
    esac
  done

  ensure_env_file
  # MosaicComposer, MediaMixer, and BusMonitor use Compose profiles, so
  # enable them for teardown as well. This removes the components when they
  # were deployed, without starting them.
  compose --profile mosaic --profile media-mixer --profile bus-monitor down "$@"
}

cmd_restart() {
  ensure_env_file

  local container_ids
  container_ids="$(compose ps --quiet --all)"

  if [[ -n "$container_ids" ]]; then
    compose restart "$@"
  else
    info "No containers exist yet; starting the stand."
    compose up --detach "$@"
  fi
}

cmd_status() {
  ensure_env_file
  compose ps --all "$@"
}

cmd_ps() {
  ensure_env_file
  compose ps "$@"
}

cmd_logs() {
  ensure_env_file
  compose logs "$@"
}

cmd_follow() {
  ensure_env_file
  compose logs --follow "$@"
}

cmd_pull() {
  ensure_env_file
  compose pull "$@"
}

cmd_build() {
  ensure_env_file
  compose build "$@"
}

cmd_clean() {
  if [[ ! -t 0 ]]; then
    err "The 'clean' command requires interactive confirmation and stdin is not a TTY."
    err "Refusing to delete volumes without confirmation."
    exit 1
  fi

  ensure_env_file

  local reply
  printf 'This will stop the %s stand and permanently delete its volumes.\n' "$PROJECT_NAME"
  printf 'Type "yes" to continue: '
  read -r reply

  case "$reply" in
    yes|YES|y|Y)
      compose --profile mosaic --profile media-mixer --profile bus-monitor down --volumes --remove-orphans "$@"
      ;;
    *)
      info "Aborted. Nothing was removed."
      ;;
  esac
}

main() {
  if (( $# == 0 )); then
    usage >&2
    exit 1
  fi

  local command="$1"
  shift

  case "$command" in
    help|-h|--help)
      usage
      return 0
      ;;
    up|down|restart|status|ps|logs|follow|pull|build|clean)
      ;;
    *)
      err "Unknown command: $command"
      usage >&2
      exit 2
      ;;
  esac

  if ! require_docker_compose; then
    exit 1
  fi

  case "$command" in
    up) cmd_up "$@" ;;
    down) cmd_down "$@" ;;
    restart) cmd_restart "$@" ;;
    status) cmd_status "$@" ;;
    ps) cmd_ps "$@" ;;
    logs) cmd_logs "$@" ;;
    follow) cmd_follow "$@" ;;
    pull) cmd_pull "$@" ;;
    build) cmd_build "$@" ;;
    clean) cmd_clean "$@" ;;
  esac
}

main "$@"
