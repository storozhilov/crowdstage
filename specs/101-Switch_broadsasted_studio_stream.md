In MediaMixer implement a switching of broadcasted video stream in the `studio`
LiveKit room. So the user is able to choose what video stream from the studio
is being forwarded to the `broadcast` LiveKit room.

MediaMixer should report in `GET /health`:

- LiveKit room participant identity whose video stream is broadcasted currently,
- The list of LiveKit room participant identities who are publishing video
  streams to the `studio` LiveKit room.

If the broadcasted stream disappears MediaMixer should automatically switch
broadcasting to another one which is active.

In MixerConsole implement a broacasting video stream switch in the **Studio**
panel. It should happen on click on the one of it's three video-players so the
bradcasted stream should switch respectively.  Currently broacasted UI slot
should be somehow selected: frame or background color or whatever.

Implement a `ci/media-mixer/switch_source.sh` Bash script which accepts at
least one CLI argument: LiveKit room participant identity whose video stream is
to be broadcasted.
