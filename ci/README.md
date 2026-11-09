# Development stand harness

This directory contains a small Bash-based harness for managing the local
development stand with Docker Compose, plus the tooling that builds the Go
application, the MosaicComposer, and the StudioMonitor web application inside
containers. The stand runs a LiveKit server; MosaicComposer and StudioMonitor
are deployed separately with the scripts in `ci/mosaic/` and
`ci/studio-monitor/`. PostgreSQL, Redis, and other client applications will be
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

## Building the applications

Go and Node.js are not installed on the host. The application and
MosaicComposer source live in `app/` and `mosaic/`, and the StudioMonitor
source lives in `studio-monitor/`. The build runs in a local builder image
based on the pinned `golang:1.26.10-trixie` image, with Node.js copied from the
pinned `node:22.22.0-trixie-slim` image.

```bash
./ci/build.sh
```

The script builds the builder image when needed and writes the application and
MosaicComposer binaries to `build/crowdstage` and `build/mosaic-composer`. It
also installs the StudioMonitor npm dependencies, runs TypeScript and ESLint
static analysis, and writes the web bundle to `studio-monitor/dist`. It works
from any directory.

The builder image is created by `ci/ensure_builder.sh`. It is tagged for the
current host user and group, and containers run as that non-root user by
default, so files written through bind mounts stay owned by the developer. The
image is reused when it already exists; pass `-f` or `--force` to rebuild it
unconditionally, which is required after changing `ci/builder/Dockerfile`. The
script prints the resulting image tag to stdout.

The builder image also contains `ffmpeg` for decoding received test video, the
GStreamer development packages and tools needed to build MosaicComposer, and
Node.js, npm, and the Playwright Chromium browser for building and testing
StudioMonitor. MosaicComposer is built with cgo enabled; the CrowdStage
application is built with cgo disabled. No media tools, runtimes, or browser
are required on the host.

Participant media tests run in a separate end-to-end image created by
`ci/e2e/ensure_image.sh`. It is based on the same pinned `livekit/gstreamer`
runtime image as MosaicComposer and adds the Go toolchain, build dependencies,
and `ffmpeg`. It also runs as the current host user and group and is reused when
it already exists; pass `-f` or `--force` to rebuild it after changing its
Dockerfile. The test script mounts the source tree and starts the participant
GStreamer pipeline directly in this container, so it does not need access to the
host Docker socket.

Go module and build caches are persisted between builds in `.cache/go-mod` and
`.cache/go-build`. Participant tests use a separate `.cache/go-build-e2e` cache
because they compile against the runtime image's GStreamer libraries. The npm
cache is kept in `.cache/npm`. These directories are ignored by Git and can be
deleted at any time to force a clean build.

CI runs the same `./ci/build.sh`, `./ci/run_tests.sh`, and
`./ci/studio-monitor/test.sh` scripts from `.github/workflows/ci.yml`.

## Running tests

The Go tests can be run on their own inside the project test containers:

```bash
./ci/run_tests.sh
```

Any arguments are passed to `go test` inside those containers:

```bash
./ci/run_tests.sh -run TestGreeting -v
./ci/run_tests.sh -count=1
```

The script uses the same builder image and Go caches as `./ci/build.sh`. It
runs tests for both the `app` and `mosaic` modules, including the MosaicComposer
health and static-background end-to-end tests after MosaicComposer has been
deployed. It reads `MOSAIC_URL` from `ci/.env` and passes the participant and
studio room names to the test container; set `MOSAIC_URL` to override the
endpoint. The background test uses the builder's `ffprobe` and `ffmpeg` to
verify the received VP8 track and its static decoded frames.

The participant overlay test runs in the separate end-to-end image because it
needs the `livekitwebrtcsink` GStreamer plugin to publish a test video. It joins
and leaves the participant room several times and verifies that the participant
appears in and disappears from the composed studio video. The multi-participant
test varies the active participant count from zero to four. The participant
pipeline runs directly inside that container; MosaicComposer itself stays on the
development stand.

```bash
./ci/dev.sh up --wait
./ci/mosaic/deploy.sh
./ci/run_tests.sh
```

### StudioMonitor tests

StudioMonitor has Node unit tests for its HTTP server and a browser end-to-end
test driven by Playwright. Both run inside the builder image, and the browser
test uses Docker host networking so it can reach StudioMonitor, MosaicComposer,
and LiveKit on the development stand.

```bash
./ci/studio-monitor/test.sh
```

The end-to-end test stops MosaicComposer, loads StudioMonitor, and verifies that
the page stays connected and waits for a stream while the composer is idle. It
then starts MosaicComposer, verifies that the video appears and playback
advances, and stops the composer again.

### Shell helper tests

The shared helpers and the MosaicComposer REST scripts have small
Docker-independent test harnesses:

```bash
./ci/test_lib.sh
./ci/mosaic/test_rest.sh
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
`LIVEKIT_URL` by default, and runs the tests in the builder container with
Docker host networking. CI runs the same commands after starting the stand and
deploying MosaicComposer.

## MosaicComposer

`MosaicComposer` is a Go backend component in `mosaic/`. On startup it joins
the participant room (`MOSAIC_ROOM`) on the development stand without
auto-subscribing to participant media. The media pipeline is idle until it is
started through the REST API. When started, a GStreamer pipeline publishes a
static 1280x720 VP8 background at 30 frames per second to the studio room
(`MOSAIC_STUDIO_ROOM`) through `livekitwebrtcsink`. Each participant video
publication gets its own `livekitwebrtcsrc`, configured to subscribe to that
participant's producer, and a tile in a centered grid that stretches across the
screen with spacing between cells and the screen borders. The grid defaults to
4x3 (twelve tiles); `POST /start` accepts a JSON body with `columns` and `rows`
to configure a different grid. Additional participants wait until a tile is
freed by someone leaving. Adding or removing a participant only changes that
participant's source and tile. It serves three REST endpoints:

- `POST /start` starts the GStreamer media pipeline and publishes the static
  background. The optional JSON body accepts positive `columns` and `rows`
  values, defaulting to `4` and `3`. The grid must fit the 1280x720 canvas and
  may contain at most 100 tiles. Calling it again is a no-op.
- `POST /stop` tears down the GStreamer pipeline and unpublishes the background.
  Calling it again is a no-op.
- `GET /health` returns subsystem health as JSON. The response includes the
  LiveKit URL, participant room, participant identity, current connection state,
  the mosaic state (`idle` or `running`), and the current participant count. The
  endpoint returns HTTP 200 when the participant room connection is established
  and HTTP 503 when it is degraded.

The component is managed with three scripts:

```bash
./ci/mosaic/ensure_image.sh      # build the Docker image if it does not exist
./ci/mosaic/ensure_image.sh -f   # rebuild the image unconditionally
./ci/mosaic/deploy.sh            # start the LiveKit stand if needed, then deploy
./ci/mosaic/uninstall.sh         # remove MosaicComposer from the stand
```

By default the endpoints are available at `MOSAIC_URL`
(`http://127.0.0.1:7980`), and the component connects to LiveKit from inside
the Compose network at `ws://livekit:7880`. The scripts use the same fixed
Compose project and `ci/.env` file as `dev.sh`.

The runtime image is based on the pinned `livekit/gstreamer:1.26.11-prod-rs`
image, which provides `livekitwebrtcsrc`, `livekitwebrtcsink`, and the VP8
encoder. It also contains a locally built `gst-plugin-webrtc` 0.14.5 with a
one-line signaller fix that lets `livekitwebrtcsrc` subscribe to a specific
producer with `signaller::producer-peer-id`. The patch and build image
definition are under `ci/mosaic/gst-plugin/`; the image downloads the pinned
0.14.5 crate, verifies its SHA-256, applies the patch, and builds the plugin.
The fix removes an early `return` from `on_participant` in
`src/livekit_signaller/imp.rs`. The image runs as the host user and group, just
like the builder image, so no media tools are required on the host.

```bash
curl -X POST http://127.0.0.1:7980/start
curl -X POST -H 'Content-Type: application/json' \
  -d '{"columns":4,"rows":3}' http://127.0.0.1:7980/start
curl -X POST http://127.0.0.1:7980/stop
curl http://127.0.0.1:7980/health
```

Example health response:

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
    },
    "mosaic": {
      "state": "running",
      "participants": 3
    }
  }
}
```

The end-user tests are `TestHealthEndpointEndToEnd` in `mosaic/e2e_test.go`,
`TestMosaicComposerPublishesStaticBackground` in
`mosaic/background_e2e_test.go`, and
`TestMosaicComposerShowsParticipantOverlayAcrossJoinDisconnectCycles` in
`mosaic/participant_e2e_test.go`. The first calls the deployed health endpoint
and verifies the JSON response. The second starts the pipeline through the REST
API, joins the studio room, receives the composer video track, verifies with
`ffprobe` that it is a 1280x720 VP8 stream, and decodes the frames with `ffmpeg`
to check that the picture is static. It then stops the pipeline and verifies
that the composer video track is no longer published to the studio room. The
participant tests publish video, check the participant grid, and disconnect
participants again. One test runs three isolated join/disconnect cycles; the
other joins several participants and performs a deterministic random sequence of
joins and leaves while verifying that the number of visible tiles matches the
number of connected participants. The health test also verifies that the mosaic
state changes from `idle` to `running` and that the participant count is
reported. The health and background tests run in the builder container; the
participant tests run in the dedicated end-to-end image.
CI deploys MosaicComposer after starting the stand, runs the tests, and
uninstalls the component during cleanup.

## StudioMonitor

`StudioMonitor` is a Node.js web application in `studio-monitor/`. It serves a
full-screen browser page and a small backend that issues LiveKit tokens, so the
API secret stays on the server. The browser joins the studio room
(`MOSAIC_STUDIO_ROOM`) with a subscribe-only token and displays the composer
video when it is published. While the room has no video, the page reports that
it is waiting for the studio stream instead of failing.

The component is managed with the same scripts used for MosaicComposer:

```bash
./ci/studio-monitor/ensure_image.sh      # build the Docker image if it does not exist
./ci/studio-monitor/ensure_image.sh -f   # rebuild the image unconditionally
./ci/studio-monitor/deploy.sh            # start the LiveKit stand if needed, then deploy
./ci/studio-monitor/uninstall.sh         # remove StudioMonitor from the stand
./ci/studio-monitor/test.sh              # run unit and browser end-to-end tests
```

Deployment prints the application URL, which defaults to
`STUDIO_MONITOR_URL` (`http://127.0.0.1:7990/`). The browser uses
`STUDIO_MONITOR_LIVEKIT_URL` (the host LiveKit URL by default) to connect. The
scripts use the same fixed Compose project and `ci/.env` file as `dev.sh`.

The runtime image is based on the pinned `node:22.22.0-trixie-slim` image and
contains only the production server dependency. The web bundle is built ahead
of time by `./ci/build.sh`.

The Node unit tests cover configuration validation, the health endpoint, token
issuing, and HTTP error handling. The Playwright test
`studio monitor waits while the composer is idle and plays after it starts`
loads the deployed page, verifies the waiting state while MosaicComposer is
stopped, starts MosaicComposer, verifies the video is playing and advancing,
then stops the composer and verifies that the page returns to the waiting
state. CI deploys StudioMonitor after starting the stand, runs the tests, and
uninstalls the component during cleanup.

## Initial setup

From the repository root:

```bash
cp ci/.env.example ci/.env
```

Edit `ci/.env` if you need different ports, URLs, credentials, or a different
LiveKit version. Every CI script loads `ci/.env` (falling back to
`ci/.env.example`) through `ci/lib.sh`, so the manual copy is optional. The
`dev.sh` script also creates `ci/.env` from `ci/.env.example` automatically the
first time a command needs it. Keep `ci/.env` out of version control; it is
ignored by `ci/.gitignore`.

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
| `./ci/e2e/ensure_image.sh` | Build the participant end-to-end test image if needed. |
| `./ci/mosaic/deploy.sh` | Deploy MosaicComposer to the stand. |
| `./ci/mosaic/uninstall.sh` | Remove MosaicComposer from the stand. |
| `./ci/studio-monitor/ensure_image.sh` | Build the StudioMonitor image if needed. |
| `./ci/studio-monitor/deploy.sh` | Deploy StudioMonitor to the stand. |
| `./ci/studio-monitor/test.sh` | Run StudioMonitor unit and browser tests. |
| `./ci/studio-monitor/uninstall.sh` | Remove StudioMonitor from the stand. |

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
- `ci/lib.sh` - shared paths, configuration loading, Docker checks, environment
  helpers, and Compose wrapper. Every CI script sources it.
- `ci/compose/docker-compose.yml` - Compose definition of the development stand.
- `ci/mosaic/` - MosaicComposer image, deployment, REST, and test scripts.
- `ci/studio-monitor/` - StudioMonitor image, deployment, and test scripts.
- `ci/builder/` - builder image used for builds, static analysis, and tests.
- `ci/e2e/` - participant end-to-end test image with the GStreamer LiveKit sink.
- `ci/.env.example` - documented defaults for all environment variables.
- `ci/.env` - local environment overrides (not committed).

`ci/lib.sh` loads `ci/.env.example` first and then `ci/.env`, so the example
provides defaults while local values override them. Variables already set in the
environment take precedence over both files. After loading, it derives
`LIVEKIT_URL`, `MOSAIC_URL`, and `STUDIO_MONITOR_URL` from the resolved port
values so that changing only `LIVEKIT_PORT`, `MOSAIC_PORT`, or
`STUDIO_MONITOR_PORT` updates the corresponding URL. The browser-facing
`STUDIO_MONITOR_LIVEKIT_URL` defaults to the host `LIVEKIT_URL`. Set the URL
variables explicitly only when an endpoint differs from the derived default.

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
| `LIVEKIT_URL` | `ws://127.0.0.1:${LIVEKIT_PORT}` | Optional host WebSocket URL used by the scripts and tests to reach LiveKit. Derived from `LIVEKIT_PORT` unless set explicitly. |
| `LIVEKIT_RTC_TCP_PORT` | `7881` | Host and container port for the WebRTC TCP fallback. |
| `LIVEKIT_RTC_UDP_PORT_START` | `50000` | First UDP port used for WebRTC media. |
| `LIVEKIT_RTC_UDP_PORT_END` | `50060` | Last UDP port used for WebRTC media. |
| `LIVEKIT_USE_EXTERNAL_IP` | `false` | Whether LiveKit should discover and advertise the host's public IP. Keep `false` for local development. |
| `LIVEKIT_API_KEY` | `devkey` | Development API key. |
| `LIVEKIT_API_SECRET` | `devsecret` | Development API secret. |
| `LIVEKIT_LOG_LEVEL` | `info` | LiveKit log level: `debug`, `info`, `warn`, or `error`. |
| `MOSAIC_PORT` | `7980` | Host port for the MosaicComposer health endpoint. |
| `MOSAIC_URL` | `http://127.0.0.1:${MOSAIC_PORT}` | Optional host URL used by the scripts and tests to reach the MosaicComposer REST API. Derived from `MOSAIC_PORT` unless set explicitly. |
| `MOSAIC_ROOM` | `mosaic-composer` | Participant LiveKit room joined by MosaicComposer. |
| `MOSAIC_STUDIO_ROOM` | `studio` | Studio LiveKit room where MosaicComposer publishes the background. |
| `MOSAIC_IDENTITY` | `mosaic-composer` | LiveKit participant identity used by MosaicComposer. |
| `STUDIO_MONITOR_PORT` | `7990` | Host port for the StudioMonitor web application. |
| `STUDIO_MONITOR_URL` | `http://127.0.0.1:${STUDIO_MONITOR_PORT}` | Optional host URL used by the scripts and tests to reach StudioMonitor. Derived from `STUDIO_MONITOR_PORT` unless set explicitly. |
| `STUDIO_MONITOR_LIVEKIT_URL` | `ws://127.0.0.1:${LIVEKIT_PORT}` | LiveKit WebSocket URL used by the StudioMonitor browser. Defaults to the host `LIVEKIT_URL`. |
| `STUDIO_MONITOR_IDENTITY` | `studio-monitor` | Identity prefix for StudioMonitor browser sessions. |

The default credentials are for local development only. Change them before
exposing the stand to any shared or public network. StudioMonitor issues
subscribe-only LiveKit tokens without authentication; this prototype is meant
for the local development stand, not for a public network.

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

StudioMonitor exposes the browser application at `http://127.0.0.1:7990/` and
its own health endpoint at `http://127.0.0.1:7990/health`. Its image has the
same kind of Docker healthcheck.

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
- `golang:1.26.10-trixie` for the builder and the end-to-end image's Go
  toolchain.
- `livekit/gstreamer:1.26.11-prod-rs` for the MosaicComposer runtime and the
  participant end-to-end test image.
- `node:22.22.0-trixie-slim` for the builder Node.js toolchain and the
  StudioMonitor runtime.
- `rust:1.85-slim-bookworm` for the patched `gst-plugin-webrtc` build.

Locally built images:

- `crowdstage-builder:uid<id>-gid<id>` for builds, static analysis, and tests.
- `crowdstage-e2e:uid<id>-gid<id>` for participant media tests.
- `crowdstage-gst-plugin-webrtc:0.14.5-patched` for the MosaicComposer runtime.
- `crowdstage-mosaic-composer:dev` for MosaicComposer.
- `crowdstage-studio-monitor:dev` for StudioMonitor.

`./ci/dev.sh pull` downloads the stand images. `./ci/mosaic/ensure_image.sh`
also builds the patched GStreamer plugin image before packaging MosaicComposer.
That image downloads the pinned crate from crates.io and verifies its SHA-256.
`./ci/mosaic/ensure_image.sh`, `./ci/e2e/ensure_image.sh`, and
`./ci/studio-monitor/ensure_image.sh` use `./ci/build.sh` to produce their build
output and package it into runtime images, which run as the host UID and GID;
Docker also downloads base images when they are not present locally.

## Scope

This harness intentionally does not include PostgreSQL, Redis, or other client
applications yet. When its media pipeline is started through the REST API,
MosaicComposer publishes a static background to the studio room and composites
up to four participant videos into tiles. StudioMonitor displays the studio
room video in a browser. The harness does not install system packages, modify
host configuration, use Kubernetes, or deploy the platform.
