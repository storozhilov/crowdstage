FROM debian:13.2-slim

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

USER nobody
EXPOSE 7980

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
  CMD ["mosaic-composer", "healthcheck"]

ENTRYPOINT ["mosaic-composer"]
