#!/bin/bash
# Runs the whole ReelStash test suite: Go backend + web UI.
# Usage: scripts/test.sh [backend|app|admin|ui]   (ui = app + admin)
#
# The backend integration tests need a Postgres they can freely write to. This uses a separate
# `reelstash_test` database inside the dev container (the one scripts/dev.sh starts, reelstash-dev-postgres), never `reelstash`.
# Point REELSTASH_TEST_DATABASE_URL somewhere else to use your own; without it those tests skip.

set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WHAT="${1:-all}"

if [ "$WHAT" = "all" ] || [ "$WHAT" = "backend" ]; then
  if [ -z "$REELSTASH_TEST_DATABASE_URL" ] && command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx reelstash-dev-postgres; then
    docker exec reelstash-dev-postgres psql -U reelstash -tc "SELECT 1 FROM pg_database WHERE datname='reelstash_test'" | grep -q 1 \
      || docker exec reelstash-dev-postgres psql -U reelstash -c "CREATE DATABASE reelstash_test" >/dev/null
    export REELSTASH_TEST_DATABASE_URL="postgres://reelstash:reelstash@localhost:${POSTGRES_PORT:-5432}/reelstash_test?sslmode=disable"
  fi
  if [ -z "$REELSTASH_TEST_DATABASE_URL" ]; then
    echo "==> REELSTASH_TEST_DATABASE_URL is not set and reelstash-dev-postgres is not running: database tests will be skipped." >&2
  fi
  echo "==> Go backend"
  (cd "$ROOT/backend" && go vet ./... && go test ./... -count=1)
fi

for APP in app admin; do
  if [ "$WHAT" = "all" ] || [ "$WHAT" = "ui" ] || [ "$WHAT" = "$APP" ]; then
    echo "==> reelstash-$APP"
    (cd "$ROOT/$APP" && { [ -d node_modules ] || npm ci; } && npm test)
  fi
done
