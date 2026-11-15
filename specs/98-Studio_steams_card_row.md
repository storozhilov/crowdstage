In **Studio** panel of the `MixerConsole` display all the video streams whish
are published to the `studio` LiveKit room.

Consider using a _Card Row_ UI design pattern. Let's have, say, a 3 equal areas
in a row on the **Studio** panel. When new video stream appears in the `studio`
LiveKit the next unused area is to be used for placing a video-player on it.

If having more than 3 video streams in the `studio` LiveKit room a navigation
button[s] should appear on the _Card Row_'s sides. So the user is able to move
the carousel back and forth: video-players should switch video sources
respectively. No not loop the carousel - let's respective navigation button
either to be hidden or disabled.

The video stream disappearance case should be processed correctly.
Video-players should gravitate to the left border of the **Studio** panel.
An empty area could not be between two active ones.

Consider using `ci/media-mixer/run_studio.sh` script for E2E testing.
