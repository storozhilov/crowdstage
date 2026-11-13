Implement a single participant join and disconnect feature.

Create a `ci/mosaic/run_guest.sh` Bash script which accepts an optional
execution timeout CLI param. If timeout is omitted scripts runs until aborted
by user (Ctrl+C).

In `MosaicComposer` implement a participants tracking system. When new
participant has joined it's minimized video media should be placed in top-right
corner by overlaying the background picture. On participant disconnect
it's video should disappear from the studio monitor view.

GStreamer pipeline should read/write media from/to the studio LiveKit room
directly using `livekitwebrtcsink` and `livekitwebrtcsrc` modules.

`MosaicComposer` should **NOT** read/write media from/to the LiveKit. It should
manipulate GStreamer pipeline using GST Go API to achieve a desired behavior.

Implement an end-to-end test where use `ci/mosaic/run_guest.sh` script to
emulate participant's join/disconnect. Test should verify multiple
join/disconnect cycles.

Current limitation: only one active participant is supported. The shared
`livekitwebrtcsrc` is recreated on disconnect to avoid stale pad metadata, so
concurrent participant overlays are outside the scope of this iteration.
