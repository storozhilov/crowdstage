FROM debian:13.2-slim

ARG USER_ID=1000
ARG GROUP_ID=1000

RUN groupadd --gid "${GROUP_ID}" mosaic \
    && useradd --uid "${USER_ID}" --gid "${GROUP_ID}" --create-home --shell /bin/bash mosaic

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates \
        libgstreamer1.0-0 \
        libgstreamer-plugins-base1.0-0 \
        gstreamer1.0-plugins-base \
        gstreamer1.0-plugins-good \
        gstreamer1.0-plugins-bad \
    && rm -rf /var/lib/apt/lists/*

COPY mosaic-composer /usr/local/bin/mosaic-composer

USER mosaic
EXPOSE 7980

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
  CMD ["mosaic-composer", "healthcheck"]

ENTRYPOINT ["mosaic-composer"]
