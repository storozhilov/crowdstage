In MixerConsole when clicked on the Guests carousel item it's video stream
should be without flickering displayed during N seconds on the broadcasted
video stream which is published by MediaMixer to the broadcast LiveKit room.

N - is a MediaMixer parameter (name it to your taste) which could be optionally
specified as a parameter to MediaMixer start REST call. This parameter should
be reported by the respective half of the MixerConsole **Control** panel. Add
a respective CLI param processing to the `ci/media-mixer/start.sh` script.
