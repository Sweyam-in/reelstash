#!/usr/bin/env bash
# Run ReelStash natively for local development: Go API + Vite UI on the host.
# PostgreSQL: scripts/dev-db.sh uses one already listening on POSTGRES_PORT, or starts a container.
#
# Usage: scripts/dev.sh          start everything (Ctrl+C stops the app)
#        scripts/dev.sh stop     prints how to stop the database container
#
# Config: .env.local (created from .env.local.example on first run).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"


if [[ "${1:-}" == "stop" ]]; then
  echo "The database container was left running. Stop it with: docker stop reelstash-dev-postgres"
  exit 0
fi

if [[ ! -f .env.local ]]; then
  cp .env.local.example .env.local
  echo "==> Created .env.local from .env.local.example"
fi
set -a; source .env.local; set +a

command -v docker >/dev/null 2>&1 || { echo "Docker is required for the local PostgreSQL container." >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "Go is required." >&2; exit 1; }

PG_PORT="${POSTGRES_PORT:-5432}"
export DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER:-reelstash}:${POSTGRES_PASSWORD:-reelstash}@localhost:${PG_PORT}/${POSTGRES_DB:-reelstash}?sslmode=disable}"
export REELSTASH_THUMBNAIL_DIR="${REELSTASH_THUMBNAIL_DIR:-$ROOT/backend/data/thumbnails}"
export REELSTASH_POSTER_DIR="${REELSTASH_POSTER_DIR:-$ROOT/backend/data/posters}"

"$ROOT/scripts/dev-db.sh" ensure "${POSTGRES_DB:-reelstash}" "${POSTGRES_USER:-reelstash}" "${POSTGRES_PASSWORD:-reelstash}" "$PG_PORT"

echo "==> API on http://localhost${REELSTASH_ADDR:-:8902}"
(cd backend && exec go run ./cmd/server) &
API_PID=$!

sleep 1
echo "==> reelstash-app on http://localhost:3000"
(cd app && exec npm start) &
UI_PID=$!
echo "==> reelstash-admin on http://localhost:3001/admin"
(cd admin && exec npm start) &
ADMIN_PID=$!

echo ""
echo "  API    : http://localhost${REELSTASH_ADDR:-:8902}"
echo "  App    : http://localhost:3000"
echo "  Admin  : http://localhost:3001/admin"
[[ "${EMERGENCY_LOCAL_AUTH:-false}" == "true" ]] && echo "  Login  : emergency local password = ${EMERGENCY_LOCAL_PASSWORD:-changeme}"
echo "  Ctrl+C stops the app; the database keeps running."
echo ""

trap 'echo "Stopping..."; kill $API_PID $UI_PID $ADMIN_PID 2>/dev/null; exit 0' INT TERM
wait
