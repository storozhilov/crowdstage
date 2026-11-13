Make `MediaMixer` to automatically track media publishing changes in studio
LiveKit room in the same manner as `MosaicComposer` does it.

When having a published video stream[s] in studio LiveKit room `MediaMixer`
should forward the "oldest" one (which was published first) to the broadcast
LiveKit room.

When no video streams are published to studio LiveKit room `MediaMixer`
should stream a static picture - the same as before.

**Inputs** `MediaMixer` run-time property should reflect the actual amount of
published video stream[s] in studio LiveKit room.

De-duplicate Go code where needed.

Consider using `ci/media-mixer/run_studio.sh` script in E2E tests which emulates
a media stream which is coming from the studio location to the studio LiveKit
room.
