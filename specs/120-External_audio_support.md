Create a `ci/media-mixer/run_audio.sh` script which is to stream
to **Studio** LiveKit room a testing looped audio pattern, preferrably
random - similar to what happens in `ci/media-mixer/run_guest.sh`, but
related to sound.

`MediaMixer` should track a LiveKit participants in the **Studio** room who
publishes audio track[s] only. New detected audio sources should be unmuted as
for now.

`MediaMixer` should report an information about all these detected audio
sources in response to `GET /health` request, including:

- Participant identity,
- Mute flag,
- Solo flag,
- Gain level,
- Signal level indicator value.

`MediaMixer` should expose a REST API to manipulate these audio-sources, including:

- Mute/Unmute,
- Solo/Unsolo,
- Set gain level.

All audio-sources from the studio should be mixed in one stereo track and sent
to the **Broadcast** LiveKit room by the broadcast participant: both video
track and audio track.

Implement an E2E tests which are to use `ci/media-mixer/run_audio.sh` script
to emulate an audio streams from the studio. Tests should track the media
streams in **Broadcast** room behave as expected in terms of an audio mix.

Let's scope these changes by the `MediaMixer` so far - no need to update other
components if possible.
