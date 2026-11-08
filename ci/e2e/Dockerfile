FROM golang:1.26.10-trixie AS go

FROM livekit/gstreamer:1.26.11-prod-rs

ARG USER_ID=1000
ARG GROUP_ID=1000

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        ffmpeg \
        git \
        libdw-dev \
        libglib2.0-dev \
        libunwind-dev \
        pkg-config \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --gid "${GROUP_ID}" e2e \
    && useradd --uid "${USER_ID}" --gid "${GROUP_ID}" --create-home --shell /bin/bash e2e

COPY --from=go /usr/local/go /usr/local/go

ENV PATH="/usr/local/go/bin:${PATH}"
ENV CGO_ENABLED=1
ENV GOFLAGS=-buildvcs=false

WORKDIR /src

USER e2e
