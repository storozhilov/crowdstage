Implement the following scripts for manual testing:

- `ci/mosaic/start.sh` which calls `POST /start` REST endpoint of
  `MosaicComposer` running in a dev stand;
- `ci/mosaic/stop.sh` which calls `POST /stop` REST endpoint;
- `ci/mosaic/get_health.sh` which calls `GET /health` REST endpoint; and prints
  out a received information in a human-readable form;
- `ci/mosaic/studio_monitor.sh` which starts an HTTP-server which displays
  a single page with the media player which on host a media player (hopefully
  `ffmpeg`) which is to playback to the Ubuntu Desktop what happens in studio
  LiveKit room - this is for human's monitoring;
- `ci/mosaic/run_participant.sh` which starts a media player which generates a
  looped testing media stream and sends it to the participants' LiveKit room.
  It should accepts an optional timeout argument otherwise runs until explicitly
  terminated (Ctrl+C). This script colud be used in autotests as well later on.

No need to create an auto-tests for these scripts, just check they work fine.

Scripts should run commands in Docker containers spawn-up from the `builder`
image. Add missing components installation to `builder` image if needed.
