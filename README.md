# CrowdStage

**Turn your audience into part of the show.**

CrowdStage is an open-source platform for interactive live events, virtual studios, and audience-driven broadcasting.

The project explores a production workflow in which remote guests are more than passive viewers: their cameras, voices, and live reactions can become part of the show.

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
| Mosaic composer | Compose selected guest video into studio-ready layouts | GStreamer |
| Production mixer | Scenes, titles, transitions, programme output | OBS integration |
| Studio control | Guest selection, Preview/Program, call-in and audio controls | Web application |
| Studio display | Full-screen display of composed audience video | Web application |

This is a design direction, not a fixed technology commitment. See [Architecture notes](docs/ARCHITECTURE.md).

## Intended first milestone

The first milestone is deliberately narrow:

1. Connect a browser publisher to a LiveKit room.
2. Join the room from a server-side MosaicComposer.
3. Publish a studio-ready output from MosaicComposer back into LiveKit.
4. Receive that output in a second browser.

The current prototype publishes a static background to the mosaic target room
and overlays up to four connected guests in a 2x2 grid in the top-right
region until they disconnect. Guest selection and configurable layouts are not
implemented yet. A browser-based BusMonitor joins a configured LiveKit room and
displays the media streamed to it full-screen. The development stand runs one
BusMonitor for the mosaic output room and one for the broadcast room. A
MixerConsole web application provides an operator control panel for the
MediaMixer and MosaicComposer subsystems, plus a Broadcast panel that plays the
stream published to the broadcast room.

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
and `build/mosaic-composer`. The BusMonitor and MixerConsole web bundles are
written to `bus-monitor/dist` and `mixer-console/dist`.

Run the tests on their own with:

```bash
./ci/run_tests.sh
```

Any arguments are passed to `go test`. Unit, health, and background tests run in
the builder container; the guest media test runs in a dedicated end-to-end
image with the LiveKit GStreamer sink. The tests connect to the LiveKit server
and MosaicComposer on the local development stand, so start and deploy them
first:

```bash
./ci/dev.sh up --wait
./ci/mosaic/deploy.sh
./ci/run_tests.sh
```

MosaicComposer is a Go backend component in `mosaic/`. It joins the guest
room on the development stand and uses a GStreamer
`livekitwebrtcsrc`/`livekitwebrtcsink` pipeline to publish a static 1280x720 VP8
background to the mosaic target room. When guests publish video, their streams
fill a centered mosaic grid that stretches across the screen with spacing
between cells and the screen borders. Each tile shows the guest name (or
identity) in a label strip below the video. The grid defaults to 4x3 (twelve
tiles); `POST /start` accepts a JSON body with `columns` and `rows` to configure
a different grid. The dimensions must fit the 1280x720 canvas, and the total
capacity is limited to 100 tiles. Additional guests wait until a tile is
freed by someone leaving or until the rotation timeout replaces a visible
guest. `POST /start` also accepts an optional positive
`rotation_timeout_seconds` value, defaulting to 10 seconds. When guests
are waiting, one tile is rotated per timeout interval: the oldest visible tile
is replaced by the first waiting guest, and the replaced guest moves
to the back of the waiting queue. Each guest video is subscribed with its own
`livekitwebrtcsrc`, so joining or leaving only adds or removes that
guest's tile while the rest of the grid keeps running. The media pipeline
is idle until it is started with `POST /start`; `POST /stop` tears it down and
unpublishes the background.
It also serves a `GET /health` JSON endpoint that reports whether the mosaic is
`idle` or `running`, the current guest count, and the active grid
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

The health, static-background, and guest grid end-to-end tests run as
part of `./ci/run_tests.sh`. The background test receives the studio track,
verifies its VP8 format and dimensions, checks that the decoded picture is
static, and confirms that `POST /stop` unpublishes the track. The guest
tests publish and disconnect test videos and verify that the mosaic tiles appear
and disappear as guests join and leave, that a small grid rotates waiting
guests, and that the health endpoint reports the current mosaic state,
guest count, active grid dimensions, and rotation timeout.
To publish a test guest manually, pass an optional display name with
`./ci/mosaic/run_guest.sh --name 'Vasya Pupkin'`; without a name, the
guest identity is used. Each guest publishes a randomly selected
test pattern so simultaneous guests look different; set
`MOSAIC_GUEST_PATTERN` to force a specific `videotestsrc` pattern.
Supported patterns are `smpte`, `snow`, `black`, `white`, `red`, `green`,
`blue`, `checkers-1`, `checkers-2`, `checkers-4`, `checkers-8`, `circular`,
`blink`, `smpte75`, `zone-plate`, `gamut`, `chroma-zone-plate`, `solid-color`,
`ball`, `smpte100`, `bar`, `pinwheel`, `spokes`, `gradient`, `colors`, and
`smpte-rp-219`. Invalid patterns are rejected before the stream starts.

## BusMonitor

BusMonitor is a small web application that joins a configured LiveKit room and
displays the media streamed to it full-screen in a browser. The same image is
used by two development-stand instances: one monitors the mosaic output room
(`MOSAIC_TARGET_ROOM`, default `mosaic`) and one monitors the broadcast room
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

## MixerConsole

MixerConsole is the operator web application for the studio. Its control panel
contains separate Media mixer and Mosaic composer sub-panels. Each sub-panel has
an on/off switch that starts or stops its subsystem and displays the parameters
reported by that subsystem's health endpoint. Below the control panel, a
Broadcast panel plays the media stream that MediaMixer publishes to the
broadcast LiveKit room. The Node backend calls the MediaMixer and
MosaicComposer REST APIs and issues subscribe-only LiveKit tokens, so the
browser only talks to MixerConsole's own REST API and never receives the LiveKit
API secret.

Build, deploy, test, and remove it with:

```bash
./ci/mixer-console/ensure_image.sh
./ci/mixer-console/deploy.sh
./ci/mixer-console/test.sh
./ci/mixer-console/uninstall.sh
```

Deployment prints the application URL, which defaults to
`http://127.0.0.1:7991/`. The browser end-to-end tests verify that each
sub-panel stays off while its subsystem is idle, starts and stops that subsystem
independently, refreshes the monitored parameters, and plays the MediaMixer
output in the Broadcast panel while MediaMixer is running.

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
