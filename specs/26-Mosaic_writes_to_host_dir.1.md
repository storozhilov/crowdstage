Fix blocking issue found in the PR:

Finding:

- `/stop` is not reliable enough to actually stop forwarding.
  - In `mosaic/forwarding.go`, `stopPipeline()` calls `entry.cancel()`, but the RTP loop in `forwardRTP()` is blocked on `source.ReadRTP()` and only checks `ctx.Done()` before the next read.
  - Because `ReadRTP()` is a blocking call, cancellation does not interrupt it immediately. The goroutine can continue publishing to the studio room for a while after /stop returns, and the forwarded track can remain published long enough to leak stale media across repeated start/stop cycles.
  - This is the same class of issue the PR is trying to fix, and it means the shutdown path is still racy.

Suggested fix:

- Do not rely on `ctx.Done()` alone while blocked in `ReadRTP()`.
- Either:
  - close/stop the underlying `webrtc.TrackRemote`/local track when stopping, or
  - restructure the loop to read RTP in a goroutine and select on a stop channel/context so cancellation can abort promptly.

