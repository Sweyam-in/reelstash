#!/usr/bin/env bash
# Run the local stack in Docker Compose (API + UI).
#
# Usage: scripts/dev-docker.sh [up|down|logs|build]   (default: up)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[[ -f .env.local ]] || { cp .env.local.example .env.local; echo "==> Created .env.local from .env.local.example"; }
mkdir -p "$(grep -E '^MEDIA_ROOT=' .env.local | cut -d= -f2- | sed "s#^\./#$ROOT/#" | head -1)" 2>/dev/null || true

compose() { docker compose --env-file .env.local "$@"; }
case "${1:-up}" in
  up)    compose up --build ;;
  down)  compose down ;;
  logs)  compose logs -f --tail=200 ;;
  build) compose build ;;
  *) echo "Usage: $0 [up|down|logs|build]" >&2; exit 2 ;;
esac
