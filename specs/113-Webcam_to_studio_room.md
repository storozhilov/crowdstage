In **Studio** panel of MixerConsole implement a support for adding/removing
to/from the list of studio streams any of the available to browser camera
devices on the operator's workstation.

After adding a stream the video media should go from the camera to the `studio`
LiveKit room through the web-browser.

Implement a `ci/media-mixer/setup_camera.sh` script which configures a virtual
camera that web browsers detect as a webcam. Configure a couple of virtual USB
cameras in the MixerConsole E2E tests docker image.

Implement a `ci/media-mixer/run_camera.sh` script which streams a testing
pattern video through the particular virtual camera. Use this script in
MixerConsole E2E tests to emulate cameras' operations.

Implement a `ci/media-mixer/list_cameras.sh` script which lists the
created virtual cameras.

Implement a `ci/media-mixer/remove_camera.sh` script which removes the
particular virtual camera.
