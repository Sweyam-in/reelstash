# syntax=docker/dockerfile:1
# reelstash-media: the ReelStash media library (image reelstash-ui).
# Built with Node, served by an nginx that runs as an unprivileged user (uid 101).
FROM docker.io/library/node:24-alpine3.24 AS builder
WORKDIR /app
COPY package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM docker.io/nginxinc/nginx-unprivileged:1.29-alpine-slim
# The base image can lag behind Alpine's security fixes (OpenSSL, zlib, musl):
# apply them, then drop back to the unprivileged nginx user.
USER root
RUN apk upgrade --no-cache
USER nginx
COPY nginx/api.conf nginx/security.conf /etc/nginx/reelstash/
COPY nginx/media.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 3091
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 CMD wget -q --spider http://127.0.0.1:3091/ || exit 1
CMD ["nginx", "-g", "daemon off;"]
