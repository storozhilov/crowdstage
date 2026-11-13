# CrowdStage

**Turn your audience into part of the show.**

CrowdStage is an open-source platform for interactive live events, virtual studios, and audience-driven broadcasting.

The project explores a production workflow in which remote participants are more than passive viewers: their cameras, voices, and live reactions can become part of the show.

> **Project status:** Early development. The repository is being prepared for public collaboration. Features and architecture described here are goals, not a claim that they are already implemented.

## Why CrowdStage?

Most online event platforms are designed primarily for one-way broadcasting or conventional video calls. CrowdStage aims to bring these two formats together:

- A production team runs a virtual or physical studio.
- Speakers and selected audience members participate over real-time media.
- Operators choose which people or groups appear on studio displays.
- The audience can react, speak, and be invited into the show.
- The resulting programme can be sent to a conventional livestream audience.

## Design direction

The initial architecture is expected to evolve around these components:

| Component | Responsibility | Candidate technology |
| --- | --- | --- |
| Interactive media | Real-time audio/video for speakers, operators, and interactive audience members | LiveKit / WebRTC |
| Audience broadcast | One-to-many delivery to passive viewers | HLS / LL-HLS |
| Mosaic composer | Compose selected participant video into studio-ready layouts | GStreamer |
| Production mixer | Scenes, titles, transitions, programme output | OBS integration |
| Studio control | Participant selection, Preview/Program, call-in and audio controls | Web application |
| Studio display | Full-screen display of composed audience video | Web application |

This is a design direction, not a fixed technology commitment. See [Architecture notes](docs/ARCHITECTURE.md).

## Intended first milestone

The first milestone is deliberately narrow:

1. Connect a browser publisher to a LiveKit room.
2. Join the room from a server-side MosaicComposer.
3. Publish a studio-ready output from MosaicComposer back into LiveKit.
4. Receive that output in a second browser.

The current prototype publishes a static background to the studio room and
overlays up to four connected participants in a 2x2 grid in the top-right region
until they disconnect. Participant selection and configurable layouts are not
implemented yet. A browser-based BusMonitor joins a configured LiveKit room and
displays the media streamed to it full-screen. The development stand runs one
BusMonitor for the mosaic output room and one for the broadcast room.

## Repository layout

```text
.
├── .github/
│   ├── ISSUE_TEMPLATE/
│   └── PULL_REQUEST_TEMPLATE.md
├── docs/
│   ├── ARCHITECTURE.md
│   └── ROADMAP.md
├── .editorconfig
├── .gitattributes
├── .gitignore
├── CODE_OF_CONDUCT.md
├── CONTRIBUTING.md
├── LICENSE
├── README.md
├── SECURITY.md
└── SUPPORT.md
```

The implementation layout will be added as the first working components land.

## Building

The project builds inside Docker; Go is not required on the host:

```bash
./ci/build.sh
```

The application and MosaicComposer binaries are written to `build/crowdstage`
and `build/mosaic-composer`. The BusMonitor web bundle is written to
`bus-monitor/dist`.

Run the tests on their own with:

```bash
./ci/run_tests.sh
```

Any arguments are passed to `go test`. Unit, health, and background tests run in
the builder container; the participant media test runs in a dedicated end-to-end
image with the LiveKit GStreamer sink. The tests connect to the LiveKit server
and MosaicComposer on the local development stand, so start and deploy them
first:

```bash
./ci/dev.sh up --wait
./ci/mosaic/deploy.sh
./ci/run_tests.sh
```

MosaicComposer is a Go backend component in `mosaic/`. It joins the participant
room on the development stand and uses a GStreamer
`livekitwebrtcsrc`/`livekitwebrtcsink` pipeline to publish a static 1280x720 VP8
background to the studio room. When participants publish video, their streams
fill a centered mosaic grid that stretches across the screen with spacing
between cells and the screen borders. Each tile shows the participant name (or
identity) in a label strip below the video. The grid defaults to 4x3 (twelve
tiles); `POST /start` accepts a JSON body with `columns` and `rows` to configure
a different grid. The dimensions must fit the 1280x720 canvas, and the total
capacity is limited to 100 tiles. Additional participants wait until a tile is
freed by someone leaving or until the rotation timeout replaces a visible
participant. `POST /start` also accepts an optional positive
`rotation_timeout_seconds` value, defaulting to 10 seconds. When participants
are waiting, one tile is rotated per timeout interval: the oldest visible tile
is replaced by the first waiting participant, and the replaced participant moves
to the back of the waiting queue. Each participant video is subscribed with its own
`livekitwebrtcsrc`, so joining or leaving only adds or removes that
participant's tile while the rest of the grid keeps running. The media pipeline
is idle until it is started with `POST /start`; `POST /stop` tears it down and
unpublishes the background.
It also serves a `GET /health` JSON endpoint that reports whether the mosaic is
`idle` or `running`, the current participant count, and the active grid
dimensions and rotation timeout while running.
Build, deploy, test, and remove it with:

```bash
./ci/mosaic/ensure_image.sh
./ci/mosaic/deploy.sh
./ci/run_tests.sh
./ci/mosaic/uninstall.sh
```

The runtime image is based on the pinned `livekit/gstreamer:1.26.11-prod-rs`
image, which provides the LiveKit GStreamer source and sink elements. It also
includes a locally built `gst-plugin-webrtc` 0.14.5 with a one-line fix so
`livekitwebrtcsrc` can subscribe to a specific participant producer. The
endpoints are available at `http://127.0.0.1:7980`:

```bash
curl -X POST http://127.0.0.1:7980/start
curl -X POST -H 'Content-Type: application/json' \
  -d '{"columns":4,"rows":3,"rotation_timeout_seconds":5}' http://127.0.0.1:7980/start
curl -X POST http://127.0.0.1:7980/stop
curl http://127.0.0.1:7980/health
```

The health, static-background, and participant grid end-to-end tests run as
part of `./ci/run_tests.sh`. The background test receives the studio track,
verifies its VP8 format and dimensions, checks that the decoded picture is
static, and confirms that `POST /stop` unpublishes the track. The participant
tests publish and disconnect test videos and verify that the mosaic tiles appear
and disappear as participants join and leave, that a small grid rotates waiting
participants, and that the health endpoint reports the current mosaic state,
participant count, active grid dimensions, and rotation timeout.
To publish a test participant manually, pass an optional display name with
`./ci/mosaic/run_participant.sh --name 'Vasya Pupkin'`; without a name, the
participant identity is used.

## BusMonitor

BusMonitor is a small web application that joins a configured LiveKit room and
displays the media streamed to it full-screen in a browser. The same image is
used by two development-stand instances: one monitors the mosaic output room
(`MOSAIC_STUDIO_ROOM`, default `studio`) and one monitors the broadcast room
(`MEDIA_MIXER_TARGET_ROOM`, default `broadcast`). The room can be set with the
`--room` command-line argument or the `BUS_MONITOR_ROOM` configuration
variable. It serves its own subscribe-only access token from a Node backend, so
the LiveKit API secret is never sent to the browser.

Build, deploy, test, and remove each instance with its harness set:

```bash
./ci/bus-monitor/mosaic/ensure_image.sh
./ci/bus-monitor/mosaic/deploy.sh
./ci/bus-monitor/mosaic/test.sh
./ci/bus-monitor/mosaic/uninstall.sh

./ci/bus-monitor/broadcast/ensure_image.sh
./ci/bus-monitor/broadcast/deploy.sh
./ci/bus-monitor/broadcast/test.sh
./ci/bus-monitor/broadcast/uninstall.sh
```

Deployment prints the application URL, which defaults to
`http://127.0.0.1:7990/` for the mosaic instance and
`http://127.0.0.1:7992/` for the broadcast instance. The browser end-to-end
tests verify that each monitor stays connected while its media source is idle,
starts playing video after the source is started, and returns to waiting when
the source is stopped again.

See [ci/README.md](ci/README.md) for the development stand and build tooling.

## Getting involved

CrowdStage welcomes contributors interested in real-time media, WebRTC, video processing, broadcasting, user experience, and event production.

- Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.
- Use GitHub Issues for reproducible bugs and focused feature proposals.
- For security-sensitive reports, follow [SECURITY.md](SECURITY.md).

## License

CrowdStage is licensed under the **GNU Affero General Public License v3.0 or later** (`AGPL-3.0-or-later`). See [LICENSE](LICENSE).

The license permits commercial use, modification, and redistribution, subject to its terms. In particular, users interacting remotely with a modified version must be offered the corresponding source code.

## Name and tagline

**CrowdStage** — *Turn your audience into part of the show.*
