In `mosaic/` subdir implement a `MosaicComposer` backend component skeleton code.

When running it should connect LiveKit running in dev stand and provide just
one `GET /health` REST-endpoint which returns a subsystem health information in
JSON format including LiveKit connection info.

`MosaicComposer` build should happen in `ci/build.sh` script.

Implement a `ci/mosaic/ensure_image.sh` script which ensures a Docker image
with all the components including the `MosaicComposer`.

Implement a `ci/mosaic/deploy.sh` script which deploys `MosaicComposer` to the dev stand.

Implement a `ci/mosaic/uninstall.sh` script which uninstalls `MosaicComposer` from the dev stand.

Implement an end user test which calls `MosaicComposer` health REST endpoint and
verifies received JSON data. This test should be executed by `ci/run_tests.sh`
script - no need to introduce a additional scripts for that.

`ci/run_tests.sh` should compose and provide a `MOSAIC_URL` envvar to tests in Docker container.

Update CI set-up respectively.

Put common bash code and functions to the `ci/lib.sh` and source it where needed.
