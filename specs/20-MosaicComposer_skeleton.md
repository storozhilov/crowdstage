In `mosaic/` subdir implement a `MosaicComposer` backend component skeleton code.

When running it should connect LiveKit and provide just one `GET /health`
REST-endpoint which returns a subsystem health information in JSON format
including LiveKit connection info.

Implement a `ci/mosaic/ensure_image.sh` script which ensures a Docker image
with all the components including the `MosaicComposer`.

Implement a `ci/mosaic/deploy.sh` script which deploys `MosaicComposer` to the dev stand.

Implement a `ci/mosaic/uninstall.sh` script which uninstalls `MosaicComposer` from the dev stand.

Implement an end user test which calls `MosaicComposer` health REST endpoint and
verifies received JSON data.

Update CI set-up respectively.
