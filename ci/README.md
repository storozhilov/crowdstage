# Development stand harness

This directory contains a small Bash-based harness for managing the local
development stand with Docker Compose, plus the tooling that builds the Go
application inside a container. At this stage the stand contains only a LiveKit
server; GStreamer, the Mosaic Composer, PostgreSQL, Redis, and client
applications will be added in later steps.

## Requirements

- Linux (the script is written for Ubuntu 22.04/24.04) or another environment
  with a compatible Bash 4+.
- Docker Engine with the Docker Compose V2 plugin (`docker compose`).
- A running Docker daemon that the current user can access.
- No `sudo` is invoked automatically by the harness.

Check the installation with:

```bash
docker --version
docker compose version
docker info
```

If the daemon is not accessible, start Docker and make sure your user is
allowed to use it. On Linux this usually means membership in the `docker`
group followed by a new login session.

## Building the Go application

Go is not installed on the host. The Go module and application source live in
`app/`, and the build runs in a local builder image based on the pinned
`golang:1.26.10-trixie` image.

```bash
./ci/build.sh
```

The script builds the builder image when needed, runs the Go tests, and writes
the application binary to `build/crowdstage`. It works from any directory.

The builder image is created by `ci/ensure_builder.sh`. It is tagged for the
current host user and group, and containers run as that non-root user by
default, so files written through bind mounts stay owned by the developer. The
image is reused when it already exists; pass `-f` or `--force` to rebuild it
unconditionally. The script prints the resulting image tag to stdout.

Go module and build caches are persisted between builds in `.cache/go-mod` and
`.cache/go-build`. Both directories are ignored by Git and can be deleted at
any time to force a clean build.

CI runs the same `./ci/build.sh` script from `.github/workflows/ci.yml`.

## Initial setup

From the repository root:

```bash
cp ci/.env.example ci/.env
```

Edit `ci/.env` if you need different ports, credentials, or a different
LiveKit version. The `dev.sh` script also creates `ci/.env` from
`ci/.env.example` automatically the first time a command needs it, so the
manual copy is optional. Keep `ci/.env` out of version control; it is ignored
by `ci/.gitignore`.

Make sure the script is executable:

```bash
chmod +x ci/dev.sh
```

The script works from any current directory, for example:

```bash
/path/to/repository/ci/dev.sh help
```

## Commands

Run `./ci/dev.sh help` to see the built-in help.

| Command | Description |
| --- | --- |
| `./ci/dev.sh up` | Create and start the stand in the background. Safe to run repeatedly. |
| `./ci/dev.sh down` | Stop and remove containers and networks, keeping volumes. |
| `./ci/dev.sh restart` | Restart the stand; starts it if it does not exist yet. |
| `./ci/dev.sh status` | Show all services, including stopped ones. |
| `./ci/dev.sh ps` | Show containers belonging to the Compose project. |
| `./ci/dev.sh logs [service]` | Show logs for all services or one service. |
| `./ci/dev.sh follow [service]` | Follow logs for all services or one service. |
| `./ci/dev.sh pull` | Pull the required images. |
| `./ci/dev.sh build` | Build images defined in the Compose configuration. |
| `./ci/dev.sh clean` | Stop the stand and delete its volumes after confirmation. |
| `./ci/dev.sh help` | Show the command reference. |

Examples:

```bash
./ci/dev.sh up
./ci/dev.sh status
./ci/dev.sh logs
./ci/dev.sh logs livekit
./ci/dev.sh follow
./ci/dev.sh follow livekit
./ci/dev.sh down
./ci/dev.sh clean
```

Extra arguments after the command are passed to Docker Compose:

```bash
./ci/dev.sh up --force-recreate
./ci/dev.sh logs --tail=200 livekit
```

## Viewing logs

```bash
./ci/dev.sh logs            # all services, no follow
./ci/dev.sh logs livekit    # only LiveKit
./ci/dev.sh follow          # all services, follow mode
./ci/dev.sh follow livekit  # only LiveKit, follow mode
```

Press `Ctrl+C` to stop following logs. It does not stop the stand.

## Stopping and cleaning up

Use `down` for normal shutdown. It removes containers and networks but keeps
volumes for future runs:

```bash
./ci/dev.sh down
```

Use `clean` only when you want to remove the project's volumes as well. It
asks for confirmation and refuses to run without an interactive terminal:

```bash
./ci/dev.sh clean
```

Both commands are scoped to the fixed Compose project `event-platform-dev`.
They do not touch resources belonging to other Docker Compose projects.

## Configuration layout

- `ci/dev.sh` - command entry point and Docker dependency checks.
- `ci/compose/docker-compose.yml` - Compose definition of the development stand.
- `ci/.env.example` - documented example of environment variables.
- `ci/.env` - local environment overrides (not committed).

The harness always uses absolute paths and a fixed project name, so running it
from different directories does not create duplicate Compose projects. The
Compose command is effectively:

```bash
docker compose \
  --project-name event-platform-dev \
  --project-directory ci/compose \
  --env-file ci/.env \
  --file ci/compose/docker-compose.yml \
  <command>
```

## Environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `LIVEKIT_VERSION` | `v1.13.11` | Pinned LiveKit server image version. The image is never referenced as `latest`, so local stands stay reproducible. |
| `LIVEKIT_PORT` | `7880` | Host and container port for the LiveKit HTTP/WebSocket API and health endpoint. |
| `LIVEKIT_RTC_TCP_PORT` | `7881` | Host and container port for the WebRTC TCP fallback. |
| `LIVEKIT_RTC_UDP_PORT_START` | `50000` | First UDP port used for WebRTC media. |
| `LIVEKIT_RTC_UDP_PORT_END` | `50060` | Last UDP port used for WebRTC media. |
| `LIVEKIT_USE_EXTERNAL_IP` | `false` | Whether LiveKit should discover and advertise the host's public IP. Keep `false` for local development. |
| `LIVEKIT_API_KEY` | `devkey` | Development API key. |
| `LIVEKIT_API_SECRET` | `devsecret` | Development API secret. |
| `LIVEKIT_LOG_LEVEL` | `info` | LiveKit log level: `debug`, `info`, `warn`, or `error`. |

The default credentials are for local development only. Change them before
exposing the stand to any shared or public network.

## Health and access

LiveKit exposes:

- `http://localhost:7880/` - HTTP/WebSocket API and readiness endpoint.
- `7881/tcp` - WebRTC TCP fallback.
- the configured UDP range - WebRTC media.

The Compose service includes a healthcheck that queries `/` from inside the
container using BusyBox `wget`, which is available in the official Alpine-based
LiveKit image. Use `./ci/dev.sh status` to see the health state.

## Troubleshooting

- **Docker is not installed or not in PATH** - install Docker Engine and the
  Compose V2 plugin, then retry.
- **Docker Compose V2 is not available** - install the Compose plugin so that
  `docker compose version` succeeds.
- **Docker daemon is not reachable** - start Docker and check that your user
  has permission to access the daemon.
- **Port already in use** - change the corresponding port variables in
  `ci/.env` and run `./ci/dev.sh up` again.
- **UDP media does not work** - make sure the configured UDP range is not
  blocked and that the start/end values are set consistently in `ci/.env`.

## Scope

This harness intentionally does not include GStreamer, the Mosaic Composer,
PostgreSQL, Redis, or frontend services yet. It does not install system
packages, modify host configuration, use Kubernetes, or deploy the platform.
