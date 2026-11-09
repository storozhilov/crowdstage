# Build a patched gst-plugin-webrtc 0.14.5 for the MosaicComposer runtime.
#
# The upstream 0.14.5 release returns early from the LiveKit signaller's
# on_participant callback for publishers, so livekitwebrtcsrc with
# signaller::producer-peer-id never sends the subscription request and produces
# no video pads. fix-producer-peer-id.patch removes that early return. Upstream
# fixed it in 0.15.3, but the 0.15.x runtime images did not connect to the
# pinned livekit-server in testing, so keep the 0.14.5 plugin with this fix.

FROM rust:1.85-slim-bookworm AS build

ARG GST_PLUGIN_VERSION=0.14.5
ARG GST_PLUGIN_SHA256=81dcf7b73db298779b5216c02b1f862a010dcec9eac51799b10fbb89e447c0c9

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        libgstreamer1.0-dev \
        libgstreamer-plugins-base1.0-dev \
        libgstreamer-plugins-bad1.0-dev \
        libssl-dev \
        patch \
        pkg-config \
    && rm -rf /var/lib/apt/lists/*

RUN curl --fail --silent --show-error --location \
        --output /tmp/gst-plugin-webrtc.crate \
        "https://static.crates.io/crates/gst-plugin-webrtc/gst-plugin-webrtc-${GST_PLUGIN_VERSION}.crate" \
    && echo "${GST_PLUGIN_SHA256}  /tmp/gst-plugin-webrtc.crate" | sha256sum --check --strict - \
    && mkdir --parents /build/source \
    && tar --extract --gzip --file /tmp/gst-plugin-webrtc.crate \
        --strip-components=1 --directory /build/source \
    && rm /tmp/gst-plugin-webrtc.crate

COPY fix-producer-peer-id.patch /tmp/fix-producer-peer-id.patch
RUN patch --directory /build/source --strip=1 < /tmp/fix-producer-peer-id.patch

WORKDIR /build/source
RUN cargo build --locked --release --no-default-features --features livekit

FROM scratch
COPY --from=build /build/source/target/release/libgstrswebrtc.so /build/target/release/libgstrswebrtc.so
