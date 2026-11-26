# syntax=docker/dockerfile:1
# Two images from one Go module:
#   docker build --target media -t reelstash-media-api backend   (media library API)
#   docker build --target admin -t reelstash-admin-api backend   (admin API + background jobs)
# The default target, all, runs both in one process (reelstash-server).
#
# Each image starts as root only long enough for docker-entrypoint.sh to make
# /app-data writable for PUID:PGID (default 1000:1000; set them to the owner of
# your media), then drops to that user for good. PUID=0 keeps root, for setups
# whose media only root can write.
FROM golang:1.27.4-alpine3.24 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# VERSION is reported to apps by /auth/server-info.
ARG VERSION=dev
# Builds reelstash-server, reelstash-media-server and reelstash-admin-server:
# static, without debug info or build paths.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for cmd in server media-server admin-server; do \
      CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X reelstash/backend/internal/httpapi.Version=${VERSION}" -o /out/reelstash-$cmd ./cmd/$cmd || exit 1; \
    done

FROM alpine:3.24 AS runtime
ARG TARGETARCH
# ffmpeg/ffprobe for probing, thumbnails and transcoding; python3 runs plugin
# bundles written in Python (see docs/plugin-bundles.md); su-exec drops root.
# On amd64, libva-intel-driver (i965, Ivy Bridge..Coffee Lake), intel-media-driver
# (iHD, Broadwell+) and mesa-va-gallium give ffmpeg VAAPI when /dev/dri is passed
# through (compose.gpu.yml); those Intel packages only exist for amd64.
RUN apk upgrade --no-cache \
 && apk add --no-cache ca-certificates tzdata ffmpeg python3 su-exec \
 && arch="${TARGETARCH:-$(uname -m)}" \
 && case "$arch" in amd64|x86_64) apk add --no-cache libva-intel-driver intel-media-driver mesa-va-gallium libva-utils ;; esac \
 && addgroup -S -g 1000 reelstash && adduser -S -D -H -u 1000 -G reelstash -h /app-data reelstash \
 && mkdir -p /app-data/thumbnails /app-data/posters /app-data/sprites /app-data/previews /app-data/subtitles /app-data/plugins /app-data/hls \
 && chown -R reelstash:reelstash /app-data
COPY --chmod=0755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
ENV PUID=1000 PGID=1000
STOPSIGNAL SIGTERM

# The media server streams, remuxes and transcodes and runs sign-in plugins.
# It runs no scans or jobs; it only needs to read your media.
FROM runtime AS media
COPY --from=builder /out/reelstash-media-server /usr/local/bin/reelstash-media-server
ENV REELSTASH_ADDR=:8902
EXPOSE 8902
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=5 CMD wget -q --spider http://127.0.0.1:8902/healthz || exit 1
ENTRYPOINT ["docker-entrypoint.sh", "reelstash-media-server"]

# The admin server runs scans, thumbnails, metadata matching, schedules,
# downloads and every plugin bundle.
FROM runtime AS admin
COPY --from=builder /out/reelstash-admin-server /usr/local/bin/reelstash-admin-server
ENV REELSTASH_ADDR=:8903
EXPOSE 8903
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 CMD wget -q --spider http://127.0.0.1:8903/healthz || exit 1
ENTRYPOINT ["docker-entrypoint.sh", "reelstash-admin-server"]

# Everything in one process.
FROM runtime AS all
COPY --from=builder /out/reelstash-server /usr/local/bin/reelstash-server
ENV REELSTASH_ADDR=:8902
EXPOSE 8902
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 CMD wget -q --spider http://127.0.0.1:8902/healthz || exit 1
ENTRYPOINT ["docker-entrypoint.sh", "reelstash-server"]
