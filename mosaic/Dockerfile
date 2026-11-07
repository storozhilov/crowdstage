FROM livekit/gstreamer:1.26.11-prod-rs

ARG USER_ID=1000
ARG GROUP_ID=1000

COPY --chown=${USER_ID}:${GROUP_ID} mosaic-composer /usr/local/bin/mosaic-composer
RUN chmod 0755 /usr/local/bin/mosaic-composer

ENV HOME=/tmp
USER "${USER_ID}:${GROUP_ID}"

EXPOSE 7980

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
  CMD ["mosaic-composer", "healthcheck"]

ENTRYPOINT ["mosaic-composer"]
