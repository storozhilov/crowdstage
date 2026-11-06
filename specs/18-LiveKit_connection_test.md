Using LiveKit SDK for Go implement a test which verifies the authenticated connection to LiveKit
running on dev stand could be established. Update CI setup where needed: merge two existing jobs in one.

This tests should be executed by `ci/run_tests.sh` script, no need to create a specific script for that.
No need to ask user to provid a value in `LIVEKIT_URL` envvar. LiveKit running on dev stand is to be used for testing.
