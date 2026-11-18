In **Studio** panel of MixerConsole implement a support for adding/removing
to/from the list of studio streams any of the available to browser camera
devices on the operator's workstation.

After adding a stream the video media should go from the camera to the `studio`
LiveKit room through the web-browser.

Implement a `ci/media-mixer/run_camera.sh` script which:

- Configures a virtual camera that web browsers detect as a webcam;
- Streams testing video pattern through the created virtual camera;
- On exit stops streaming and removes the created virtual camera.

The script should be container-aware: when executed on host it should run
itself in a privileged container because it needs root.

Consider calling this script (even parallel execution of multiple script runs)
in MixerConsole E2E tests when testing this feature.
