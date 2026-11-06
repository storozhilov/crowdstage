2 issues worth fixing:

1. The VP8 validation in mosaic/forwarding_e2e_test.go is not parsing the frame correctly.
  - vp8KeyframeSize() checks frame[0]&0x01 and then reads dimensions from frame[6:8] / frame[8:10].
  - That is not a reliable way to parse depacketized VP8 payloads; the first bytes are the VP8 payload descriptor/header, not a raw frame-size field.
  - Result: the test can reject valid forwarded frames or fail intermittently depending on packetization. This makes the E2E assertion flaky.
2. Shutdown leaves forwarded tracks published in the studio room.
  - In mosaic/forwarding.go, closeForwards() only calls entry.cancel() and never calls UnpublishTrack(...).
  - That means on shutdown/restart, the local studio publications can linger until the room disconnects; the goroutine may be cancelled but the underlying LiveKit track remains published.
  - This is a resource leak and can leave stale forwarded media in the studio room during repeated test runs.

