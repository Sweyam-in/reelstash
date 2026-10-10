FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# VERSION is reported to apps by /auth/server-info.
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X reelstash/backend/internal/httpapi.Version=${VERSION}" -o /out/reelstash-server ./cmd/server

FROM alpine:3.20
# libva-intel-driver (legacy i965, Ivy Bridge..Coffee Lake) and
# intel-media-driver (iHD, Broadwell+) cover old and new Intel iGPUs alike;
# mesa-va-gallium adds a fallback VAAPI driver for other GPUs. libva-utils
# provides `vainfo` for diagnosing driver/device issues in a running
# container. None of this does anything unless /dev/dri is passed through
# (see docker-compose.prod.yml) — ffmpeg still runs software-only otherwise.
# python3 runs plugin bundles written in Python (such as the ones in the
# reelstash_plugins repo); see docs/plugin-bundles.md.
RUN apk add --no-cache ffmpeg ca-certificates python3 \
    libva-intel-driver intel-media-driver mesa-va-gallium libva-utils
COPY --from=builder /out/reelstash-server /usr/local/bin/reelstash-server
VOLUME ["/app-data/thumbnails"]
EXPOSE 8902
ENTRYPOINT ["reelstash-server"]
