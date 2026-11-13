Instead of `StudioMonitor` implement a `BusMonitor` web-application
which displays to user what media is streamed to the specified LiveKit room.

`BusMonitor` should work with mosaic and broadcasting LiveKit rooms. Which
LiveKit room to track is specified by the CLI arg or config during the deployment.

Create two sets of harness scripts for two instances of this web-app which are
deployed to dev stand: one is for the mosaic LiveKit room and another is for
broadcasting LiveKit room.
