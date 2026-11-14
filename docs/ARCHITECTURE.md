# Architecture notes

This document records the current design direction. It is intentionally lightweight and will change as implementation decisions are validated.

## Product model

CrowdStage targets interactive online events where a production team can bring remote guests into a studio-style programme.

There are two distinct audience roles:

- **Interactive guests** publish and/or receive real-time audio and video.
- **Passive viewers** watch the programme broadcast and do not need a persistent WebRTC connection.

Keeping these roles separate should help the platform scale without treating every viewer as an active video-call participant.

## Proposed media flow

```text
Interactive guests
          |
          v
     LiveKit SFU
          |
          v
   Mosaic Composer
          |
          v
     LiveKit SFU ------> Studio Display
          |
          +------------> Production Mixer (e.g. OBS)
                                  |
                                  v
                         HLS / CDN viewers
```

The diagram is conceptual. Exact routing and integration points remain to be validated.

## Main components

### Interactive media

WebRTC sessions for hosts, speakers, operators, and audience members invited to participate. LiveKit is the current candidate SFU.

### Mosaic Composer

A server-side media component that subscribes to selected guest tracks and publishes one or more composed outputs. It should support multiple layouts over time, for example:

- Grid
- Featured guest
- Selected guests
- Speaker plus audience
- Caller
- Discussion

The first technical milestone should prove a single-track relay before introducing composition.

### Studio Control

An operator-facing application for guest selection, layout control, and Preview/Program workflows.

The current prototype is `MixerConsole` in `mixer-console/`: a Node.js web
application with a control panel for the broadcasting subsystems. It has
separate Media mixer and Mosaic composer sub-panels, each with its own on/off
switch and monitored health parameters. The browser talks only to
MixerConsole's REST API; the backend calls the MediaMixer and MosaicComposer
REST APIs inside the Compose network.

### Studio Display

A dedicated full-screen client for venue displays. It should consume a composed output rather than subscribe to many individual guest cameras.

The current prototype is `BusMonitor` in `bus-monitor/`: a small Node.js web
application that issues subscribe-only LiveKit tokens and renders the media
streamed to a configured room in a browser. It waits for the stream when the
room has no media and displays the video as soon as a publisher joins. The
development stand runs one instance for the mosaic output room and one for the
broadcast room.

### Broadcast delivery

Passive viewers should ultimately receive a conventional broadcast stream, likely HLS or LL-HLS delivered through a CDN. This path is separate from the interactive WebRTC path.

## Initial engineering principles

1. Validate a complete vertical slice before building abstractions.
2. Keep control-plane responsibilities separate from media-plane processing.
3. Avoid sending hundreds of independent camera tracks to a single studio display.
4. Make guest selection and layout changes explicit and observable.
5. Design for a small number of active cameras first; measure before optimizing for the maximum audience size.
6. Treat privacy, consent, and operator control as first-class product requirements.
