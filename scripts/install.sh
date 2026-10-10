#!/bin/sh
# ReelStash installer: sets up a complete ReelStash stack (PostgreSQL, API, media
# app, admin app and, optionally, qBittorrent for the Downloads workspace) with
# Docker Compose on any Linux or macOS machine.
#
#   wget -qO- https://raw.githubusercontent.com/Sweyam-in/reelstash/main/scripts/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/Sweyam-in/reelstash/main/scripts/install.sh | sh
#
# Pass options after `sh -s --`:
#   wget -qO- <url> | sh -s -- --media /srv/media --yes
#
# Run it again in the same folder to update: .env is kept, new images are pulled.
# See `install.sh --help`. Plain POSIX sh; needs docker with the compose plugin.
set -eu

# ---- defaults (override with the options below or the same-named env vars) ----
DIR="${REELSTASH_DIR:-}"
MEDIA="${REELSTASH_MEDIA:-}"
HOST_NAME="${REELSTASH_HOST:-}"
TAG="${REELSTASH_TAG:-main}"
REGISTRY="${REELSTASH_REGISTRY:-ghcr.io/sweyam-in}"
REPO_URL="${REELSTASH_REPO:-https://github.com/Sweyam-in/reelstash.git}"
ADMIN_USER="${REELSTASH_ADMIN_USER:-admin}"
UI_PORT="${REELSTASH_UI_PORT:-3091}"
ADMIN_PORT="${REELSTASH_ADMIN_PORT:-3092}"
QBIT=""            # empty = ask, 1 = yes, 0 = no
FROM_SOURCE=0
ASSUME_YES=0
PRINT_COMPOSE=0

usage() {
	cat <<'EOF'
ReelStash installer

Usage: install.sh [options]

  --dir PATH           Install folder (default: ./reelstash)
  --media PATH         Folder with your media; mounted at /mnt in the containers
  --host NAME|IP       Address people use to reach this machine (default: detected)
  --admin-user NAME    First admin's username (default: admin); the password is generated
  --qbittorrent        Also run qBittorrent for the Downloads workspace
  --no-qbittorrent     Do not (default when running non-interactively)
  --ui-port N          Media app port (default 3091)
  --admin-port N       Admin app port (default 3092)
  --tag TAG            Image tag to install (default: main)
  --registry PREFIX    Image registry prefix (default: ghcr.io/sweyam-in)
  --from-source        Build the images from the Git repository instead of pulling
  --yes, -y            Accept defaults and never prompt
  --print-compose      Print the compose.yml this script writes, then exit
  --help, -h           Show this help

Every option also exists as an environment variable: REELSTASH_DIR, REELSTASH_MEDIA,
REELSTASH_HOST, REELSTASH_TAG, REELSTASH_REGISTRY, REELSTASH_ADMIN_USER,
REELSTASH_UI_PORT, REELSTASH_ADMIN_PORT.
EOF
}

# ---- output helpers -----------------------------------------------------------
if [ -t 1 ]; then B=$(printf '\033[1m'); G=$(printf '\033[32m'); Y=$(printf '\033[33m'); R=$(printf '\033[31m'); N=$(printf '\033[0m'); else B=""; G=""; Y=""; R=""; N=""; fi
say() { printf '%s\n' "$*"; }
step() { printf '\n%s==>%s %s%s%s\n' "$G" "$N" "$B" "$*" "$N"; }
warn() { printf '%swarning:%s %s\n' "$Y" "$N" "$*" >&2; }
die() { printf '%serror:%s %s\n' "$R" "$N" "$*" >&2; exit 1; }

# ---- compose file -------------------------------------------------------------
# Quoted heredoc: ${VAR} is expanded by Docker Compose from .env, not by this shell.
write_compose() {
	cat <<'EOF'
# Written by the ReelStash installer. Settings live in .env next to this file.
name: reelstash

x-logging: &default-logging
  driver: json-file
  options:
    max-size: "10m"
    max-file: "5"

services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: reelstash
      POSTGRES_USER: reelstash
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U reelstash -d reelstash"]
      interval: 5s
      timeout: 5s
      retries: 20
      start_period: 20s
    logging: *default-logging

  reelstash:
    image: ${REELSTASH_API_IMAGE}:${REELSTASH_IMAGE_TAG}
    restart: unless-stopped
    stop_grace_period: 30s
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://reelstash:${POSTGRES_PASSWORD}@postgres:5432/reelstash?sslmode=disable
      REELSTASH_ADDR: :8902
      TZ: ${TZ:-UTC}
      REELSTASH_THUMBNAIL_DIR: /app-data/thumbnails
      REELSTASH_POSTER_DIR: /app-data/posters
      REELSTASH_SPRITE_DIR: /app-data/sprites
      REELSTASH_PREVIEW_DIR: /app-data/previews
      REELSTASH_SUBTITLE_DIR: /app-data/subtitles
      REELSTASH_PLUGIN_DIR: /app-data/plugins
      REELSTASH_SERVER_NAME: ${REELSTASH_SERVER_NAME:-ReelStash}
      REELSTASH_MEDIA_URLS: ${REELSTASH_MEDIA_URLS:-}
      REELSTASH_ADMIN_URLS: ${REELSTASH_ADMIN_URLS:-}
      REELSTASH_ADMIN_USERNAME: ${REELSTASH_ADMIN_USERNAME:-}
      REELSTASH_ADMIN_PASSWORD: ${REELSTASH_ADMIN_PASSWORD:-}
      EMERGENCY_LOCAL_AUTH: "false"
      # Downloads workspace: set when the qbittorrent profile is on.
      REELSTASH_QBIT_URL: ${REELSTASH_QBIT_URL:-}
      REELSTASH_QBIT_USERNAME: ${QBIT_USERNAME:-admin}
      REELSTASH_QBIT_PASSWORD: ${QBIT_PASSWORD:-}
    volumes:
      - ${MEDIA_ROOT}:/mnt
      - thumbnail_data:/app-data/thumbnails
      - poster_data:/app-data/posters
      - sprite_data:/app-data/sprites
      - preview_data:/app-data/previews
      - subtitle_data:/app-data/subtitles
      - plugin_data:/app-data/plugins
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://127.0.0.1:8902/healthz"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 15s
    logging: *default-logging

  # The media library.
  ui:
    image: ${REELSTASH_UI_IMAGE}:${REELSTASH_IMAGE_TAG}
    restart: unless-stopped
    depends_on:
      reelstash:
        condition: service_healthy
    ports:
      - "${REELSTASH_UI_PORT:-3091}:3091"
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://127.0.0.1:3091/"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 10s
    logging: *default-logging

  # Admin, Files and Downloads, on their own port.
  admin:
    image: ${REELSTASH_ADMIN_IMAGE}:${REELSTASH_IMAGE_TAG}
    restart: unless-stopped
    depends_on:
      reelstash:
        condition: service_healthy
    ports:
      - "${REELSTASH_ADMIN_PORT:-3092}:3091"
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://127.0.0.1:3091/admin"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 10s
    logging: *default-logging

  # Downloads workspace (profile "downloads", switched on by COMPOSE_PROFILES in .env).
  # qbittorrent-init writes the WebUI login into qBittorrent's config before it starts.
  qbittorrent-init:
    image: ${REELSTASH_API_IMAGE}:${REELSTASH_IMAGE_TAG}
    profiles: ["downloads"]
    entrypoint: ["reelstash-server", "qbit-init", "/config/qBittorrent/qBittorrent.conf"]
    environment:
      REELSTASH_QBIT_USERNAME: ${QBIT_USERNAME:-admin}
      REELSTASH_QBIT_PASSWORD: ${QBIT_PASSWORD:-}
    volumes:
      - qbittorrent_config:/config
    restart: "no"

  qbittorrent:
    image: lscr.io/linuxserver/qbittorrent:${QBIT_IMAGE_TAG:-latest}
    profiles: ["downloads"]
    restart: unless-stopped
    depends_on:
      qbittorrent-init:
        condition: service_completed_successfully
    environment:
      PUID: ${MEDIA_UID:-1000}
      PGID: ${MEDIA_GID:-1000}
      UMASK: "002"
      TZ: ${TZ:-UTC}
      WEBUI_PORT: 8080
      TORRENTING_PORT: ${TORRENT_PORT:-6881}
    volumes:
      - ${MEDIA_ROOT}:/mnt
      - qbittorrent_config:/config
    ports:
      # qBittorrent's own web UI. 127.0.0.1 = this machine only.
      - "${QBIT_BIND_ADDR:-127.0.0.1}:${QBIT_WEBUI_PORT:-8080}:8080"
      - "${TORRENT_PORT:-6881}:${TORRENT_PORT:-6881}"
      - "${TORRENT_PORT:-6881}:${TORRENT_PORT:-6881}/udp"
    logging: *default-logging

volumes:
  postgres_data:
  thumbnail_data:
  poster_data:
  sprite_data:
  preview_data:
  subtitle_data:
  plugin_data:
  qbittorrent_config:
EOF
}

# Optional overrides, chosen at install time and listed in COMPOSE_FILE.
write_gpu_compose() {
	cat <<'EOF'
# Hardware transcoding: hands the host's GPU render node to ffmpeg (VAAPI).
services:
  reelstash:
    devices:
      - /dev/dri:/dev/dri
EOF
}

write_build_compose() {
	cat <<'EOF'
# --from-source: build the images from ./src instead of pulling them.
services:
  reelstash:
    build: ./src/backend
  qbittorrent-init:
    build: ./src/backend
  ui:
    build: ./src/app
  admin:
    build: ./src/admin
EOF
}

# ---- arguments ----------------------------------------------------------------
while [ $# -gt 0 ]; do
	case "$1" in
	--dir) DIR="${2:?--dir needs a value}"; shift 2 ;;
	--media) MEDIA="${2:?--media needs a value}"; shift 2 ;;
	--host) HOST_NAME="${2:?--host needs a value}"; shift 2 ;;
	--admin-user) ADMIN_USER="${2:?--admin-user needs a value}"; shift 2 ;;
	--ui-port) UI_PORT="${2:?--ui-port needs a value}"; shift 2 ;;
	--admin-port) ADMIN_PORT="${2:?--admin-port needs a value}"; shift 2 ;;
	--tag) TAG="${2:?--tag needs a value}"; shift 2 ;;
	--registry) REGISTRY="${2:?--registry needs a value}"; shift 2 ;;
	--qbittorrent) QBIT=1; shift ;;
	--no-qbittorrent) QBIT=0; shift ;;
	--from-source) FROM_SOURCE=1; shift ;;
	--yes | -y) ASSUME_YES=1; shift ;;
	--print-compose) PRINT_COMPOSE=1; shift ;;
	--help | -h) usage; exit 0 ;;
	*) usage >&2; die "unknown option: $1" ;;
	esac
done

if [ "$PRINT_COMPOSE" = 1 ]; then write_compose; exit 0; fi

# ---- prompts (read from the terminal, so `... | sh` still works) ---------------
INTERACTIVE=0
if [ "$ASSUME_YES" = 0 ] && [ -r /dev/tty ] && (: </dev/tty) 2>/dev/null; then INTERACTIVE=1; fi

# ask VAR "question" "default"
ask() {
	_var=$1; _q=$2; _def=$3
	if [ "$INTERACTIVE" = 1 ]; then
		printf '%s [%s]: ' "$_q" "$_def" >/dev/tty
		IFS= read -r _ans </dev/tty || _ans=""
		[ -n "$_ans" ] || _ans=$_def
	else
		_ans=$_def
	fi
	eval "$_var=\$_ans"
}

# confirm "question" default(y|n) -> 0 for yes
confirm() {
	if [ "$INTERACTIVE" = 1 ]; then
		printf '%s [%s]: ' "$1" "$2" >/dev/tty
		IFS= read -r _ans </dev/tty || _ans=""
		[ -n "$_ans" ] || _ans=$2
	else
		_ans=$2
	fi
	case "$_ans" in y | Y | yes | YES) return 0 ;; *) return 1 ;; esac
}

# ---- helpers ------------------------------------------------------------------
random_secret() {
	# 24 URL-safe characters.
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | cut -c1-24
	else
		LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 24
	fi
}

env_get() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1; }

owner_of() {
	stat -c '%u:%g' "$1" 2>/dev/null || stat -f '%u:%g' "$1" 2>/dev/null || echo ""
}

detect_host() {
	_ip=""
	if command -v hostname >/dev/null 2>&1; then _ip=$(hostname -I 2>/dev/null | awk '{print $1}') || _ip=""; fi
	if [ -z "$_ip" ] && command -v ip >/dev/null 2>&1; then
		_ip=$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}') || _ip=""
	fi
	if [ -z "$_ip" ] && command -v ipconfig >/dev/null 2>&1; then _ip=$(ipconfig getifaddr en0 2>/dev/null) || _ip=""; fi
	echo "${_ip:-localhost}"
}

port_in_use() {
	if command -v ss >/dev/null 2>&1; then ss -ltn 2>/dev/null | awk '{print $4}' | grep -q "[:.]$1$"
	elif command -v lsof >/dev/null 2>&1; then lsof -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
	else return 1; fi
}

http_ok() {
	if command -v curl >/dev/null 2>&1; then curl -fsS -o /dev/null -m 5 "$1" 2>/dev/null
	elif command -v wget >/dev/null 2>&1; then wget -q -O /dev/null -T 5 "$1" 2>/dev/null
	else return 1; fi
}

# ---- preflight ----------------------------------------------------------------
step "Checking this machine"
command -v docker >/dev/null 2>&1 || die "docker is not installed. Install it first: https://docs.docker.com/engine/install/"
docker info >/dev/null 2>&1 || die "cannot talk to the Docker daemon. Is it running, and does your user have access? Try again with sudo, or add yourself to the docker group."
docker compose version >/dev/null 2>&1 || die "the Docker Compose plugin (v2) is missing. Install it: https://docs.docker.com/compose/install/"
say "Docker $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo ok), $(docker compose version --short 2>/dev/null || echo compose ok)"
if [ "$FROM_SOURCE" = 1 ]; then command -v git >/dev/null 2>&1 || die "--from-source needs git"; fi

[ -n "$DIR" ] || DIR="./reelstash"
mkdir -p "$DIR"
DIR=$(cd "$DIR" && pwd)
ENV_FILE="$DIR/.env"
UPGRADE=0
[ -f "$ENV_FILE" ] && UPGRADE=1

# ---- collect settings (fresh install only) -------------------------------------
if [ "$UPGRADE" = 0 ]; then
	step "Installing into $DIR"

	[ -n "$MEDIA" ] || ask MEDIA "Folder with your media files (mounted at /mnt)" "$DIR/media"
	case "$MEDIA" in /*) ;; *) MEDIA="$(pwd)/$MEDIA" ;; esac
	mkdir -p "$MEDIA" || die "cannot create $MEDIA"

	[ -n "$HOST_NAME" ] || ask HOST_NAME "Address you will open ReelStash at (IP or hostname)" "$(detect_host)"

	if [ -z "$QBIT" ]; then
		if confirm "Also set up qBittorrent for the Downloads workspace?" "n"; then QBIT=1; else QBIT=0; fi
	fi

	for p in "$UI_PORT" "$ADMIN_PORT"; do
		if port_in_use "$p"; then die "port $p is already in use. Pick another with --ui-port / --admin-port."; fi
	done
	[ "$UI_PORT" != "$ADMIN_PORT" ] || die "--ui-port and --admin-port must differ"

	PG_PASS=$(random_secret)
	ADMIN_PASS=$(random_secret)
	QBIT_PASS=""
	[ "$QBIT" = 1 ] && QBIT_PASS=$(random_secret)

	OWNER=$(owner_of "$MEDIA")
	MEDIA_UID=${OWNER%%:*}; MEDIA_GID=${OWNER##*:}
	# Root-owned media: run qBittorrent as 1000 rather than as root.
	{ [ -n "$MEDIA_UID" ] && [ "$MEDIA_UID" != 0 ]; } || { MEDIA_UID=1000; MEDIA_GID=1000; warn "$MEDIA is not owned by a regular user; qBittorrent will run as 1000:1000. Adjust MEDIA_UID/MEDIA_GID in $ENV_FILE if needed."; }

	COMPOSE_FILES="compose.yml"
	if [ -e /dev/dri ]; then
		write_gpu_compose >"$DIR/compose.gpu.yml"
		COMPOSE_FILES="$COMPOSE_FILES:compose.gpu.yml"
		say "Found /dev/dri: hardware transcoding enabled (compose.gpu.yml)."
	fi
	if [ "$FROM_SOURCE" = 1 ]; then
		write_build_compose >"$DIR/compose.build.yml"
		COMPOSE_FILES="$COMPOSE_FILES:compose.build.yml"
		API_IMAGE="reelstash-api"; UI_IMAGE="reelstash-ui"; ADMIN_IMAGE="reelstash-admin"; TAG="local"
	else
		API_IMAGE="$REGISTRY/reelstash-api"; UI_IMAGE="$REGISTRY/reelstash-ui"; ADMIN_IMAGE="$REGISTRY/reelstash-admin"
	fi

	TZ_NAME=""
	if [ -r /etc/timezone ]; then TZ_NAME=$(cat /etc/timezone); fi
	if [ -z "$TZ_NAME" ] && [ -L /etc/localtime ]; then TZ_NAME=$(readlink /etc/localtime | sed 's|.*/zoneinfo/||'); fi

	umask 077
	{
		echo "# ReelStash settings. Keep this file private (it holds passwords)."
		echo "COMPOSE_FILE=$COMPOSE_FILES"
		[ "$QBIT" = 1 ] && echo "COMPOSE_PROFILES=downloads"
		echo ""
		echo "REELSTASH_API_IMAGE=$API_IMAGE"
		echo "REELSTASH_UI_IMAGE=$UI_IMAGE"
		echo "REELSTASH_ADMIN_IMAGE=$ADMIN_IMAGE"
		echo "REELSTASH_IMAGE_TAG=$TAG"
		echo ""
		echo "MEDIA_ROOT=$MEDIA"
		echo "TZ=${TZ_NAME:-UTC}"
		echo "POSTGRES_PASSWORD=$PG_PASS"
		echo ""
		echo "REELSTASH_UI_PORT=$UI_PORT"
		echo "REELSTASH_ADMIN_PORT=$ADMIN_PORT"
		echo "REELSTASH_MEDIA_URLS=http://$HOST_NAME:$UI_PORT"
		echo "REELSTASH_ADMIN_URLS=http://$HOST_NAME:$ADMIN_PORT"
		echo ""
		echo "# First admin, created at start if the username is new. After you have signed in"
		echo "# and changed the password, you may delete the password line."
		echo "REELSTASH_ADMIN_USERNAME=$ADMIN_USER"
		echo "REELSTASH_ADMIN_PASSWORD=$ADMIN_PASS"
		if [ "$QBIT" = 1 ]; then
			echo ""
			echo "# Downloads (qBittorrent). Its own web UI is bound to this machine only;"
			echo "# set QBIT_BIND_ADDR=0.0.0.0 to reach it from the network."
			echo "REELSTASH_QBIT_URL=http://qbittorrent:8080"
			echo "QBIT_USERNAME=admin"
			echo "QBIT_PASSWORD=$QBIT_PASS"
			echo "QBIT_BIND_ADDR=127.0.0.1"
			echo "QBIT_WEBUI_PORT=8080"
			echo "TORRENT_PORT=6881"
			echo "MEDIA_UID=$MEDIA_UID"
			echo "MEDIA_GID=$MEDIA_GID"
		fi
	} >"$ENV_FILE"
	chmod 600 "$ENV_FILE"
	umask 022
else
	step "Updating the install in $DIR"
	say "Found $ENV_FILE: keeping your settings."
	# An explicit --tag / REELSTASH_TAG on an update moves the install to that tag.
	if [ "$TAG" != main ] && [ "$FROM_SOURCE" = 0 ]; then
		sed -i.bak "s|^REELSTASH_IMAGE_TAG=.*|REELSTASH_IMAGE_TAG=$TAG|" "$ENV_FILE"
		rm -f "$ENV_FILE.bak"
	fi
	HOST_NAME=$(env_get REELSTASH_MEDIA_URLS | sed 's|^http://||; s|:[0-9]*$||')
	UI_PORT=$(env_get REELSTASH_UI_PORT); ADMIN_PORT=$(env_get REELSTASH_ADMIN_PORT)
fi

# compose.yml is rewritten on every run so updates bring compose fixes along.
write_compose >"$DIR/compose.yml"

# ---- images -------------------------------------------------------------------
cd "$DIR"
if [ "$FROM_SOURCE" = 1 ]; then
	step "Fetching the source and building images (this takes a few minutes)"
	if [ -d src/.git ]; then git -C src pull --ff-only; else git clone --depth 1 "$REPO_URL" src; fi
	docker compose build
else
	step "Pulling images"
	if ! docker compose pull; then
		die "could not pull the images from $REGISTRY. If the registry is private, run 'docker login ghcr.io', or re-run with --from-source to build them locally."
	fi
fi

step "Starting ReelStash"
docker compose up -d

step "Waiting for ReelStash to come up"
i=0
until http_ok "http://127.0.0.1:$UI_PORT/healthz"; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		warn "ReelStash did not answer within 3 minutes. Check: cd $DIR && docker compose logs -f reelstash"
		exit 1
	fi
	sleep 3
done

# ---- done ---------------------------------------------------------------------
UI_URL="http://$HOST_NAME:$UI_PORT"
ADMIN_URL="http://$HOST_NAME:$ADMIN_PORT"
printf '\n%sReelStash is running.%s\n\n' "$G$B" "$N"
say "  Media app   $UI_URL"
say "  Admin       $ADMIN_URL"
if [ "$UPGRADE" = 0 ]; then
	say ""
	say "  Sign in as   ${B}$ADMIN_USER${N}"
	say "  Password     ${B}$ADMIN_PASS${N}   (also in $ENV_FILE)"
	say ""
	say "Next: open Admin, run the setup wizard, and add your media folder. Inside the"
	say "containers your media is at /mnt, so a folder on the host at"
	say "  $MEDIA/movies"
	say "is /mnt/movies in ReelStash."
	if [ "$QBIT" = 1 ]; then
		say ""
		say "Downloads: open $ADMIN_URL/downloads. qBittorrent's own web UI is on"
		say "http://127.0.0.1:8080 (user admin, password in $ENV_FILE). Forward TCP/UDP 6881 on your"
		say "router for good speeds. Create folders you move torrents into as the media owner."
	fi
	say ""
	say "This is plain HTTP. To reach it from outside your network, put a reverse proxy with"
	say "HTTPS in front of ports $UI_PORT and $ADMIN_PORT, and update REELSTASH_MEDIA_URLS and"
	say "REELSTASH_ADMIN_URLS in $ENV_FILE."
fi
say ""
say "Manage it from $DIR:"
say "  docker compose logs -f reelstash     # logs"
say "  docker compose down                  # stop (data is kept in Docker volumes)"
say "  re-run this installer to update"
