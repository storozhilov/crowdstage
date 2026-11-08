Implement the following scripts for manual testing:

- `ci/mosaic/start.sh` which calls `POST /start` REST endpoint of
  `MosaicComposer` running in a dev stand;
- `ci/mosaic/stop.sh` which calls `POST /stop` REST endpoint;
- `ci/mosaic/get_health.sh` which calls `GET /health` REST endpoint; and prints
  out a received information in a human-readable form;

No need to create an auto-tests for these scripts, just check they work fine.

Scripts should run commands in Docker containers spawn-up from the `builder`
image. Add missing components installation to `builder` image if needed.
