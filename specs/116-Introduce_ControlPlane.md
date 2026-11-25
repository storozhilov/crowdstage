Introduce a `ControlPlane` backend component which is a single REST backend for
all the components on the client side.

Current functionality of the `MixerConsole` and `BusMonitor` backend components
should be moved to the `ControlPlane`.

Set up a dedicated image for the `ControlPlane` to be built by the CI harness
automation.

Media streams are transferred separately by communicating to the LiveKit
directly. `ControlPlane` is used to control media streams, but not to transfer
them through it.

**Do not** touch the Go code. The task is related to `MixerConsole` and
`BusMonitor` backend components only.
