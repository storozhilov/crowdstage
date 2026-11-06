FROM alpine:3.24.3

RUN apk add --no-cache ca-certificates

COPY mosaic-composer /usr/local/bin/mosaic-composer

USER nobody
EXPOSE 7980

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
  CMD ["mosaic-composer", "healthcheck"]

ENTRYPOINT ["mosaic-composer"]
