Reimplement `MediaMixer` from scratch so it would just to stream a some picture to the broadcasting
LiveKit room.

Instead of reporting in `GET /health` about forwarded and waiting tracks it should report:

- **Inputs** - the amount of _LiveKit Participants_ (identities) in the studio outputs LiveKit room:
  currently `0`,
- **Outputs** - the amount of broadcasting LiveKit rooms `MediaMixer` streams the main mix to:
  currently `1`.

`MediaMixer` code should borrow concepts from the `MosaicComposer` code. Do a code de-duplication
by introducing a shared Go module for that.

`ci/media-mixer/run_studio.sh` is not needed, but consider keeping it fo the future work.
