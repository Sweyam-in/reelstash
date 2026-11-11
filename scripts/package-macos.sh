#!/usr/bin/env bash
set -euo pipefail
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stage="$repo/dist/macos-stage"
app="$repo/dist/ReelStash.app"
"$repo/scripts/package-local.sh" "$stage"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$stage/bin/reelstash-server" "$app/Contents/MacOS/reelstash-server"
cp -R "$stage/web" "$app/Contents/Resources/web"

# Jellyfin's portable macOS build is self-contained apart from macOS system
# frameworks. Pin both architectures so a packaging run is reproducible.
ffmpeg_version="8.1.3-2"
case "$(uname -m)" in
  arm64)
    ffmpeg_arch="macarm64"
    ffmpeg_sha="d898cc819e7c4e9219d6410ddbdaada0838f14bec6ff89e283639915394cb43f"
    ;;
  x86_64)
    ffmpeg_arch="mac64"
    ffmpeg_sha="d394180f1155f84857544a48a99869f5741ccc50c5a1f73d2797429a2b326448"
    ;;
  *) echo "Unsupported macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac
ffmpeg_file="jellyfin-ffmpeg_${ffmpeg_version}_portable_${ffmpeg_arch}-gpl.tar.xz"
ffmpeg_cache="$repo/dist/cache/$ffmpeg_file"
mkdir -p "$(dirname "$ffmpeg_cache")" "$app/Contents/Resources/ffmpeg"
if [[ ! -f "$ffmpeg_cache" ]] || [[ "$(shasum -a 256 "$ffmpeg_cache" | cut -d ' ' -f 1)" != "$ffmpeg_sha" ]]; then
  curl --fail --location --retry 3 --output "$ffmpeg_cache" \
    "https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v${ffmpeg_version}/$ffmpeg_file"
fi
if [[ "$(shasum -a 256 "$ffmpeg_cache" | cut -d ' ' -f 1)" != "$ffmpeg_sha" ]]; then
  echo "Jellyfin FFmpeg archive checksum mismatch" >&2
  exit 1
fi
tar -xJf "$ffmpeg_cache" -C "$app/Contents/Resources/ffmpeg" ffmpeg ffprobe
chmod +x "$app/Contents/Resources/ffmpeg/ffmpeg" "$app/Contents/Resources/ffmpeg/ffprobe"
curl --fail --location --retry 3 --output "$app/Contents/Resources/ffmpeg/LICENSE.md" \
  "https://raw.githubusercontent.com/jellyfin/jellyfin-ffmpeg/v${ffmpeg_version}/LICENSE.md"
curl --fail --location --retry 3 --output "$app/Contents/Resources/ffmpeg/COPYING.GPLv3" \
  "https://raw.githubusercontent.com/jellyfin/jellyfin-ffmpeg/v${ffmpeg_version}/COPYING.GPLv3"
cat > "$app/Contents/Resources/ffmpeg/SOURCE.txt" <<EOF
Jellyfin FFmpeg ${ffmpeg_version}, portable ${ffmpeg_arch} GPL build.
Source: https://github.com/jellyfin/jellyfin-ffmpeg/tree/v${ffmpeg_version}
Binary: https://github.com/jellyfin/jellyfin-ffmpeg/releases/tag/v${ffmpeg_version}
EOF
cat > "$app/Contents/MacOS/ReelStash" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
contents="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data="$HOME/Library/Application Support/ReelStash"
mkdir -p "$data"
export DATABASE_URL="${DATABASE_URL:-sqlite://$data/reelstash.db}"
export REELSTASH_WEB_DIR="$contents/Resources/web"
export PATH="$contents/Resources/ffmpeg:$PATH"
export REELSTASH_THUMBNAIL_DIR="${REELSTASH_THUMBNAIL_DIR:-$data/thumbnails}"
export REELSTASH_POSTER_DIR="${REELSTASH_POSTER_DIR:-$data/posters}"
export REELSTASH_ADDR="${REELSTASH_ADDR:-127.0.0.1:8902}"
export REELSTASH_PUBLIC_ORIGIN="${REELSTASH_PUBLIC_ORIGIN:-http://127.0.0.1:8902}"
"$contents/MacOS/reelstash-server" &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null || true' EXIT INT TERM
for _ in $(seq 1 50); do
  if curl --silent --fail "$REELSTASH_PUBLIC_ORIGIN/healthz" >/dev/null; then
    open "$REELSTASH_PUBLIC_ORIGIN"
    break
  fi
  sleep 0.1
done
wait "$server_pid"
EOF
chmod +x "$app/Contents/MacOS/ReelStash"
cat > "$app/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>ReelStash</string>
<key>CFBundleIdentifier</key><string>app.reelstash.local</string>
<key>CFBundleVersion</key><string>2.1.0</string>
<key>CFBundleShortVersionString</key><string>2.1.0</string>
<key>CFBundleExecutable</key><string>ReelStash</string>
<key>LSMinimumSystemVersion</key><string>12.0</string>
</dict></plist>
EOF
hdiutil create -volname ReelStash -srcfolder "$app" -ov -format UDZO "$repo/dist/ReelStash-macos.dmg"
echo "Built $repo/dist/ReelStash-macos.dmg"
