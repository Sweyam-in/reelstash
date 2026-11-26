# Two images from one Go module:
#   docker build --target media -t reelstash-media-api backend   (media library API)
#   docker build --target admin -t reelstash-admin-api backend   (admin API + background jobs)
# The default target, all, runs both in one process (reelstash-server).
FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# VERSION is reported to apps by /auth/server-info.
ARG VERSION=dev
# Builds reelstash-server, reelstash-media-server and reelstash-admin-server.
RUN for cmd in server media-server admin-server; do \
      CGO_ENABLED=0 go build -ldflags "-X reelstash/backend/internal/httpapi.Version=${VERSION}" -o /out/reelstash-$cmd ./cmd/$cmd || exit 1; \
    done

# libva-intel-driver (legacy i965, Ivy Bridge..Coffee Lake) and
# intel-media-driver (iHD, Broadwell+) cover old and new Intel iGPUs alike;
# mesa-va-gallium adds a fallback VAAPI driver for other GPUs. libva-utils
# provides `vainfo` for diagnosing driver/device issues in a running
# container. None of this does anything unless /dev/dri is passed through
# (see compose.gpu.yml) — ffmpeg still runs software-only otherwise.
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ffmpeg ca-certificates \
    libva-intel-driver intel-media-driver mesa-va-gallium libva-utils

# The media server streams, remuxes and transcodes (ffmpeg) and runs
# sign-in plugins; it runs no scans or jobs.
FROM runtime AS media
# python3 runs plugin bundles written in Python (see docs/plugin-bundles.md).
RUN apk add --no-cache python3
COPY --from=builder /out/reelstash-media-server /usr/local/bin/reelstash-media-server
EXPOSE 8902
ENTRYPOINT ["reelstash-media-server"]

# The admin server runs scans, thumbnails, metadata matching, schedules,
# downloads and every plugin bundle.
FROM runtime AS admin
RUN apk add --no-cache python3
COPY --from=builder /out/reelstash-admin-server /usr/local/bin/reelstash-admin-server
VOLUME ["/app-data/thumbnails"]
ENV REELSTASH_ADDR=:8903
EXPOSE 8903
ENTRYPOINT ["reelstash-admin-server"]

# Everything in one process.
FROM runtime AS all
RUN apk add --no-cache python3
COPY --from=builder /out/reelstash-server /usr/local/bin/reelstash-server
VOLUME ["/app-data/thumbnails"]
EXPOSE 8902
ENTRYPOINT ["reelstash-server"]
