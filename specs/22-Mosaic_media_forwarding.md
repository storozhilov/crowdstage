In `MosaicComposer` implement a forwarding media as is w/o any processing from
the participant's LiveKit channel to the studio LiveKit channel.

Implement an auto-test which sends testing participant's media to it's LiveKit channel,
receives it from the studio LiveKit channel and verifies it looks good.

Create a script which ensures testing data is created in project subtree using
a `builder` image. The script should accept `-f|--force` CLI param to force
testing data re-generation. Install necessary tools to `builder` image, e.g. `ffmpeg`.
