Create a `ci/media-mixer/run_studio.sh` testing script which
sends media to the studio LiveKit room.

Script should accept an optional timeout. If timeout is omitted
script should execute until explicitly interrupted (Ctrl+C).

Consider migrating media mixer E2E tests to using this script
or implement some additional tests if it makes sense.
