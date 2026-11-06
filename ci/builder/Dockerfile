FROM golang:1.26.10-trixie

ARG USER_ID=1000
ARG GROUP_ID=1000

RUN groupadd --gid "${GROUP_ID}" builder \
    && useradd --uid "${USER_ID}" --gid "${GROUP_ID}" --create-home --shell /bin/bash builder

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ffmpeg \
    && rm -rf /var/lib/apt/lists/*

ENV GOFLAGS=-buildvcs=false

WORKDIR /src

USER builder
