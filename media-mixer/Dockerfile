FROM livekit/gstreamer:1.26.11-prod-rs

ARG USER_ID=1000
ARG GROUP_ID=1000

COPY --chown=${USER_ID}:${GROUP_ID} media-mixer /usr/local/bin/media-mixer
RUN chmod 0755 /usr/local/bin/media-mixer

ENV HOME=/tmp
USER "${USER_ID}:${GROUP_ID}"

EXPOSE 7981

ENTRYPOINT ["media-mixer"]
