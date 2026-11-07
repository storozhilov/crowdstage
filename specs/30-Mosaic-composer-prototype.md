Remove media streams forwarding and recording tech spike code.

Introduce a `mosaic composer` GStreamer pipeline which is connected to
participant and studio LiveKit rooms using `livekitwebrtcsrc` and
`livekitwebrtcsink` GStreamer components.

As for now participants' media streams should be ignored. `mosaic composer`
pipeline should just broadcast to studio LiveKit room a some picture which is
to be used as a mosaic's background.

Implement an auto-test which issues `POST /start` to the `MosaicComposer`
deployed in dev stand, then checks incoming video contains a static picture and
has a proper format. Then issue `POST /stop` and check nothing is streamed to
studio LiveKit room.
