# Development stand harness

This directory contains a small Bash-based harness for managing the local
development stand with Docker Compose, plus the tooling that builds the Go
application and the MosaicComposer inside containers. The stand runs a LiveKit
server; MosaicComposer is deployed to the stand separately with the scripts in
`ci/mosaic/`. GStreamer, PostgreSQL, Redis, and client applications will be
added in later steps.

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

## Building the Go applications

Go is not installed on the host. The application and MosaicComposer source live
in `app/` and `mosaic/`, and the build runs in a local builder image based on
the pinned `golang:1.26.10-trixie` image.

```bash
./ci/build.sh
```

The script builds the builder image when needed and writes the application and
MosaicComposer binaries to `build/crowdstage` and `build/mosaic-composer`. It
works from any directory.

The builder image is created by `ci/ensure_builder.sh`. It is tagged for the
current host user and group, and containers run as that non-root user by
default, so files written through bind mounts stay owned by the developer. The
image is reused when it already exists; pass `-f` or `--force` to rebuild it
unconditionally. The script prints the resulting image tag to stdout.

Go module and build caches are persisted between builds in `.cache/go-mod` and
`.cache/go-build`. Both directories are ignored by Git and can be deleted at
any time to force a clean build.

CI runs the same `./ci/build.sh` and `./ci/run_tests.sh` scripts from
`.github/workflows/ci.yml`.

## Running tests

The Go tests can be run on their own inside the builder container:

```bash
./ci/run_tests.sh
```

Any arguments are passed to `go test` inside the container:

```bash
./ci/run_tests.sh -run TestGreeting -v
./ci/run_tests.sh -count=1
```

The script uses the same builder image and Go caches as `./ci/build.sh`. It
runs tests for both the `app` and `mosaic` modules, including the MosaicComposer
health endpoint end-to-end test after MosaicComposer has been deployed. It also
composes `MOSAIC_URL` from `MOSAIC_PORT` and passes it to the test container;
set `MOSAIC_URL` to override the default:

```bash
./ci/dev.sh up --wait
./ci/mosaic/deploy.sh
./ci/run_tests.sh
```

### Authenticated LiveKit connection test

The test `TestLiveKitAuthenticatedConnection` joins a LiveKit room using the
credentials from `ci/.env`, which verifies that a client can authenticate
against the running development stand.

Start the stand, deploy MosaicComposer, and run the tests with:

```bash
./ci/dev.sh up --wait
./ci/mosaic/deploy.sh
./ci/run_tests.sh
```

The script reads `ci/.env` for the API credentials, connects to the stand at
`ws://127.0.0.1:${LIVEKIT_PORT}` by default, and runs the tests in the builder
container with Docker host networking. CI runs the same commands after starting
the stand and deploying MosaicComposer.

## MosaicComposer

`MosaicComposer` is a Go backend component in `mosaic/`. On startup it joins the
LiveKit room on the development stand and serves one REST endpoint:

- `GET /health` returns subsystem health as JSON. The response includes the
  LiveKit URL, room, participant identity, and current connection state. The
  endpoint returns HTTP 200 when LiveKit is connected and HTTP 503 when the
  connection is degraded.

The component is managed with three scripts:

```bash
./ci/mosaic/ensure_image.sh      # build the Docker image if it does not exist
./ci/mosaic/ensure_image.sh -f   # rebuild the image unconditionally
./ci/mosaic/deploy.sh            # start the LiveKit stand if needed, then deploy
./ci/mosaic/uninstall.sh         # remove MosaicComposer from the stand
```

By default the endpoint is available at `http://127.0.0.1:7980/health`, and the
component connects to LiveKit from inside the Compose network at
`ws://livekit:7880`. The scripts use the same fixed Compose project and
`ci/.env` file as `dev.sh`.

Example response:

```json
{
  "status": "ok",
  "subsystems": {
    "livekit": {
      "status": "ok",
      "connected": true,
      "url": "ws://livekit:7880",
      "room": "mosaic-composer",
      "identity": "mosaic-composer",
      "connection_state": "connected"
    }
  }
}
```

The end-user test is `TestHealthEndpointEndToEnd` in `mosaic/e2e_test.go`. It
calls the deployed health endpoint and verifies the JSON response. It is run by
`./ci/run_tests.sh` through the same builder container and Go cache as the other
tests. CI deploys MosaicComposer after starting the stand, runs the tests, and
uninstalls the component during cleanup.

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
| `./ci/mosaic/ensure_image.sh` | Build the MosaicComposer image if needed. |
| `./ci/mosaic/deploy.sh` | Deploy MosaicComposer to the stand. |
| `./ci/mosaic/uninstall.sh` | Remove MosaicComposer from the stand. |

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

- `ci/dev.sh` - command entry point for the development stand.
- `ci/lib.sh` - shared paths, Docker checks, environment helpers, and Compose wrapper.
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
| `MOSAIC_PORT` | `7980` | Host port for the MosaicComposer health endpoint. |
| `MOSAIC_ROOM` | `mosaic-composer` | LiveKit room joined by MosaicComposer. |
| `MOSAIC_IDENTITY` | `mosaic-composer` | LiveKit participant identity used by MosaicComposer. |

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

MosaicComposer exposes `http://127.0.0.1:7980/health` on the host. Its image
also has a Docker healthcheck that calls the same endpoint from inside the
container.

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

## Container images

Externally sourced images:

- `livekit/livekit-server:${LIVEKIT_VERSION}` for the stand.
- `golang:1.26.10-trixie` for the builder and for compiling MosaicComposer.
- `alpine:3.24.3` for the MosaicComposer runtime.

Locally built images:

- `crowdstage-builder:uid<id>-gid<id>` for builds and tests.
- `crowdstage-mosaic-composer:dev` for MosaicComposer.

`./ci/dev.sh pull` downloads the stand images. `./ci/mosaic/ensure_image.sh`
uses `./ci/build.sh` to produce the MosaicComposer binary and packages it into
the image; Docker also downloads base images when they are not present locally.

## Scope

This harness intentionally does not include GStreamer, PostgreSQL, Redis, or
frontend services yet. MosaicComposer is currently a health and LiveKit
connection skeleton; it does not compose or publish media. The harness does not
install system packages, modify host configuration, use Kubernetes, or deploy
the platform.
