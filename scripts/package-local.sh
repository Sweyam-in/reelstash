#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${1:-$repo/dist/local}"
mkdir -p "$out/web" "$out/bin"

(cd "$repo/app" && npm ci && npm run build)
(cd "$repo/admin" && npm ci && npm run build)
cp -R "$repo/app/dist/." "$out/web/"
cp -R "$repo/admin/dist/." "$out/web/"
(cd "$repo/backend" && CGO_ENABLED=1 go build -trimpath -o "$out/bin/reelstash-server" ./cmd/server)

cat > "$out/bin/reelstash" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
base="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data="${REELSTASH_DATA_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/reelstash}"
mkdir -p "$data"
export DATABASE_URL="${DATABASE_URL:-sqlite://$data/reelstash.db}"
export REELSTASH_WEB_DIR="${REELSTASH_WEB_DIR:-$base/web}"
export REELSTASH_THUMBNAIL_DIR="${REELSTASH_THUMBNAIL_DIR:-$data/thumbnails}"
export REELSTASH_POSTER_DIR="${REELSTASH_POSTER_DIR:-$data/posters}"
export REELSTASH_ADDR="${REELSTASH_ADDR:-127.0.0.1:8902}"
export REELSTASH_PUBLIC_ORIGIN="${REELSTASH_PUBLIC_ORIGIN:-http://127.0.0.1:8902}"
exec "$base/bin/reelstash-server"
EOF
chmod +x "$out/bin/reelstash"
echo "Built $out"
