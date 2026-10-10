#!/bin/bash
# A throwaway ReelStash server for developing the app: its own database
# (reelstash_mobile in the dev Postgres container), sample videos it generates
# with ffmpeg, and a password admin. Runs the API from ../backend on :8912,
# which the Android emulator reaches at http://10.0.2.2:8912.
#
# Usage: scripts/dev-server.sh            start the server (Ctrl+C stops it)
#        scripts/dev-server.sh seed       in another shell: libraries + scan
#
# Test-only credentials (override with the env vars):
#   MOBILE_DEV_USER=mobile-admin  MOBILE_DEV_PASSWORD=mobile-dev-password
set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
BACKEND="$HERE/../backend"
DEV="$HERE/.dev"
PORT="${MOBILE_DEV_PORT:-8912}"
USER_NAME="${MOBILE_DEV_USER:-mobile-admin}"
PASSWORD="${MOBILE_DEV_PASSWORD:-mobile-dev-password}"
API="http://localhost:$PORT"

make_media() {
  local m="$DEV/media"
  [ -f "$m/.done" ] && return
  echo "==> Generating sample videos in $m"
  mkdir -p "$m/movies/Big Test Movie (2024)" "$m/shows/Test Show/Season 01" "$m/private" "$m/reels"
  local src="testsrc2=size=1280x720:rate=30"
  ffmpeg -loglevel error -y -f lavfi -i "$src" -f lavfi -i "sine=frequency=440" -t 120 -c:v libx264 -pix_fmt yuv420p -c:a aac \
    "$m/movies/Big Test Movie (2024)/Big Test Movie (2024).mp4"
  printf '1\n00:00:01,000 --> 00:00:06,000\nHello from a sidecar subtitle.\n\n2\n00:00:08,000 --> 00:00:14,000\nSecond line.\n' \
    > "$m/movies/Big Test Movie (2024)/Big Test Movie (2024).en.srt"
  for ep in 1 2; do
    ffmpeg -loglevel error -y -f lavfi -i "smptebars=size=1280x720:rate=25" -f lavfi -i "sine=frequency=$((300 + ep * 100))" -t 40 \
      -c:v libx264 -pix_fmt yuv420p -c:a aac "$m/shows/Test Show/Season 01/Test Show - S01E0$ep.mkv"
  done
  # MPEG-4 Part 2 in AVI: a container the app's player can't open, so it must transcode.
  ffmpeg -loglevel error -y -f lavfi -i "mandelbrot=size=640x360:rate=25" -f lavfi -i "sine=frequency=660" -t 30 \
    -c:v mpeg4 -c:a libmp3lame "$m/private/transcode-test.avi"
  ffmpeg -loglevel error -y -f lavfi -i "testsrc=size=1280x720:rate=30" -f lavfi -i "anoisesrc=amplitude=0.05" -t 60 \
    -c:v libx264 -pix_fmt yuv420p -c:a aac "$m/private/scene-one.mp4"
  ffmpeg -loglevel error -y -f lavfi -i "testsrc2=size=720x1280:rate=30" -f lavfi -i "sine=frequency=520" -t 20 \
    -c:v libx264 -pix_fmt yuv420p -c:a aac "$m/reels/portrait-reel.mp4"
  touch "$m/.done"
}

start() {
  docker exec reelstash-pg-dev psql -U reelstash -tc "SELECT 1 FROM pg_database WHERE datname='reelstash_mobile'" | grep -q 1 \
    || docker exec reelstash-pg-dev psql -U reelstash -c "CREATE DATABASE reelstash_mobile" >/dev/null
  make_media
  mkdir -p "$DEV/data/thumbnails" "$DEV/data/posters"
  echo "==> ReelStash API for the app on $API (emulator: http://10.0.2.2:$PORT)"
  cd "$BACKEND"
  DATABASE_URL="postgres://reelstash:reelstash@localhost:55432/reelstash_mobile?sslmode=disable" \
  REELSTASH_ADDR=":$PORT" \
  REELSTASH_SERVER_NAME="Mobile dev" \
  REELSTASH_THUMBNAIL_DIR="$DEV/data/thumbnails" \
  REELSTASH_POSTER_DIR="$DEV/data/posters" \
  REELSTASH_PLUGIN_BUNDLES=false \
  REELSTASH_ADMIN_USERNAME="$USER_NAME" \
  REELSTASH_ADMIN_PASSWORD="$PASSWORD" \
  EMERGENCY_LOCAL_AUTH=false \
  exec go run ./cmd/server
}

seed() {
  local token
  token=$(curl -sf "$API/auth/token" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASSWORD\",\"deviceName\":\"seed script\",\"platform\":\"script\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
  call() { curl -sf -H "Authorization: Bearer $token" -H 'Content-Type: application/json' "$@"; }
  local storage
  storage=$(call "$API/api/admin/storage" | python3 -c 'import json,sys; print(next((s["id"] for s in json.load(sys.stdin)["items"] if s["name"]=="Mobile dev media"), ""))')
  [ -z "$storage" ] && storage=$(call "$API/api/admin/storage" -d "{\"name\":\"Mobile dev media\",\"type\":\"LOCAL\",\"mountPath\":\"$DEV/media\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
  local ids=()
  for lib in "Movies|entertainment|movies" "Shows|entertainment|shows" "Private|private|private" "Reels|private|reels"; do
    IFS='|' read -r name mode path <<<"$lib"
    local id
    id=$(call "$API/api/admin/libraries" | python3 -c "import json,sys; print(next((l['id'] for l in json.load(sys.stdin)['items'] if l['name']=='$name'), ''))")
    [ -z "$id" ] && id=$(call "$API/api/admin/libraries" -d "{\"name\":\"$name\",\"mode\":\"$mode\",\"defaultAccess\":true,\"roots\":[{\"storageLocationId\":$storage,\"path\":\"$path\"}]}" \
      | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
    echo "    library $name ($mode) -> $id"
    ids+=("$id")
  done
  # One scan runs at a time; start each when the last has finished.
  for id in "${ids[@]}"; do
    until call -X POST "$API/api/admin/libraries/$id/scan" >/dev/null 2>&1; do sleep 2; done
    echo "    scanning library $id"
  done
  # No metadata provider here, so group the two episodes into a show by hand (for Up next).
  until [ "$(call "$API/api/media?kind=episode" | python3 -c 'import json,sys; print(json.load(sys.stdin)["total"])')" = "2" ]; do sleep 2; done
  local show episodes
  show=$(call "$API/api/series" | python3 -c "import json,sys; print(next((s['id'] for s in json.load(sys.stdin)['items'] if s['name']=='Test Show'), ''))")
  [ -z "$show" ] && show=$(call "$API/api/series" -d '{"name":"Test Show"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
  episodes=$(call "$API/api/media?kind=episode&sort=title" | python3 -c 'import json,sys; print(",".join(str(i["id"]) for i in json.load(sys.stdin)["items"]))')
  call "$API/api/series/$show/media" -d "{\"mediaItemIds\":[$episodes]}" >/dev/null
  echo "    Test Show -> episodes $episodes"
}

case "${1:-start}" in
  start) start ;;
  seed) seed ;;
  *) echo "usage: $0 [start|seed]" >&2; exit 2 ;;
esac
