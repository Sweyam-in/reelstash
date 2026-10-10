#!/usr/bin/env bash
# Makes sure a PostgreSQL for local development is reachable on POSTGRES_PORT.
# If something already listens there (your own Postgres, another container) it is
# used as is and must already have the role and database from .env.local.
# Otherwise a postgres:16-alpine container named reelstash-dev-postgres is started.
#
# Usage: scripts/dev-db.sh ensure DB USER PASSWORD [PORT]
set -euo pipefail

[[ "${1:-}" == "ensure" ]] || { echo "Usage: $0 ensure DB USER PASSWORD [PORT]" >&2; exit 2; }
DB="${2:-reelstash}"; USER_NAME="${3:-reelstash}"; PASS="${4:-reelstash}"; PORT="${5:-${POSTGRES_PORT:-5432}}"
NAME=reelstash-dev-postgres

listening() { (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null; }

if docker ps --format '{{.Names}}' | grep -qx "$NAME"; then
  :
elif listening; then
  echo "==> PostgreSQL already listening on localhost:$PORT, using it" >&2
  exit 0
elif docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
  docker start "$NAME" >/dev/null
else
  echo "==> Starting PostgreSQL ($NAME) on localhost:$PORT" >&2
  docker run -d --name "$NAME" --restart unless-stopped \
    -e POSTGRES_USER="$USER_NAME" -e POSTGRES_PASSWORD="$PASS" -e POSTGRES_DB="$DB" \
    -p "127.0.0.1:$PORT:5432" -v reelstash-dev-pgdata:/var/lib/postgresql/data \
    postgres:16-alpine >/dev/null
fi

for _ in $(seq 1 40); do
  docker exec "$NAME" pg_isready -U "$USER_NAME" >/dev/null 2>&1 && exit 0
  sleep 0.5
done
echo "PostgreSQL did not become ready" >&2
exit 1
