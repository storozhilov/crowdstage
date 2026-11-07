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

The current prototype publishes a static background to the studio room before
participant selection and composition are added.

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
and `build/mosaic-composer`.

Run the tests on their own with:

```bash
./ci/run_tests.sh
```

Any arguments are passed to `go test`. The tests connect to the LiveKit server
and MosaicComposer on the local development stand, so start and deploy them
first:

```bash
./ci/dev.sh up --wait
./ci/mosaic/deploy.sh
./ci/run_tests.sh
```

MosaicComposer is a Go backend component in `mosaic/`. It joins the participant
room on the development stand, deliberately ignores participant media for now,
and uses a GStreamer `livekitwebrtcsrc`/`livekitwebrtcsink` pipeline to publish
a static 1280x720 VP8 background to the studio room. The media pipeline is idle until it is
started with `POST /start`; `POST /stop` tears it down and unpublishes the
background. It also serves a `GET /health` JSON endpoint.
Build, deploy, test, and remove it with:

```bash
./ci/mosaic/ensure_image.sh
./ci/mosaic/deploy.sh
./ci/run_tests.sh
./ci/mosaic/uninstall.sh
```

The runtime image is based on the pinned `livekit/gstreamer:1.26.11-prod-rs`
image, which provides the LiveKit GStreamer source and sink elements. The
endpoints are available at `http://127.0.0.1:7980`:

```bash
curl -X POST http://127.0.0.1:7980/start
curl -X POST http://127.0.0.1:7980/stop
curl http://127.0.0.1:7980/health
```

The health and static-background end-to-end tests run as part of
`./ci/run_tests.sh`. The background test receives the studio track, verifies its
VP8 format and dimensions, checks that the decoded picture is static, and
confirms that `POST /stop` unpublishes the track.

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
