Resolve the issues worth fixing:

- High: shutdown can deadlock in `mosaic/forwarding_pipeline.go`
  - `rtpForwarder.close()` does `close(f.stop)` and then waits on `<-f.done`, but `forward()` can block in `f.target.WriteRTP(packet, nil)` after `TryPullSample()` succeeds.
  - There is no timeout or cancellation on the downstream write, so `/stop` can hang indefinitely if the room/local track is stalled or closing.
  - This is effectively the same shutdown problem class as the earlier blocking `ReadRTP()` issue, just moved one layer deeper.

