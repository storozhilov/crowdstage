FROM node:22.22.0-trixie-slim

ARG USER_ID=1000
ARG GROUP_ID=1000

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --omit=dev \
    && npm cache clean --force

COPY server.mjs ./server.mjs
COPY dist ./dist

ENV HOME=/tmp
ENV NODE_ENV=production
USER "${USER_ID}:${GROUP_ID}"

EXPOSE 7991

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
  CMD ["node", "server.mjs", "healthcheck"]

ENTRYPOINT ["node", "server.mjs"]
