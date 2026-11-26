#!/bin/sh
# Starts a ReelStash server as PUID:PGID (default 1000:1000) instead of root.
# As root it first makes the data folders writable for that user: only folders
# whose owner differs are changed, so this is quick after the first start.
set -eu

if [ "$(id -u)" != "0" ]; then
	exec "$@"
fi

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"
if [ "$PUID" = "0" ]; then
	echo "docker-entrypoint: PUID=0, running as root" >&2
	exec "$@"
fi
case "$PUID$PGID" in
	*[!0-9]*) echo "docker-entrypoint: PUID and PGID must be numbers" >&2; exit 1 ;;
esac

# Data folders: the configured ones and their shared parent (/app-data).
for dir in /app-data \
	"${REELSTASH_THUMBNAIL_DIR:-/app-data/thumbnails}" "${REELSTASH_POSTER_DIR:-/app-data/posters}" \
	"${REELSTASH_SPRITE_DIR:-/app-data/sprites}" "${REELSTASH_PREVIEW_DIR:-/app-data/previews}" \
	"${REELSTASH_SUBTITLE_DIR:-/app-data/subtitles}" "${REELSTASH_PLUGIN_DIR:-/app-data/plugins}" /app-data/hls; do
	mkdir -p "$dir"
	if [ "$(stat -c %u:%g "$dir")" != "$PUID:$PGID" ]; then
		echo "docker-entrypoint: giving $dir to $PUID:$PGID" >&2
		chown -R "$PUID:$PGID" "$dir"
	fi
done

exec su-exec "$PUID:$PGID" "$@"
