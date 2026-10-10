# ReelStash

By [Sweyam](https://sweyam.com) · [Product page](https://sweyam.com/products/reelstash) · [Releases](https://github.com/Sweyam-in/reelstash/releases)

ReelStash is a self-hosted media library and file organizer. It indexes video files on mounted storage, enriches them with metadata, and provides browser-based playback, library management, and administration.

The repository contains a Go API, a React UI, PostgreSQL migrations, background workers, and Docker deployment files. Media, Admin, and Files are three UI entry points built from the same frontend codebase.

## What it does

- Indexes mounted storage incrementally, reads media details with `ffprobe`, imports NFO metadata, and generates thumbnails, preview clips, and scrub sprites.
- Organizes media into libraries, including entertainment and private library modes, with per-user access control.
- Supports search, playlists, favorites, ratings, watch history, watch later, recommendations, and direct or HLS playback.
- Matches metadata through configurable providers (TMDB built in; others as plugins), and provides review and rename workflows.
- Runs scans, duplicate detection, metadata matching, and organization as persisted background jobs, on demand, on a cron schedule, or when files appear on a watched disk.
- Picks up where you left off (Continue watching, Next up, and an "Up next" countdown between episodes) and plays in your language: audio track and subtitle selection, with per-user defaults.
- Builds manual and smart playlists; a smart playlist fills itself from saved filters such as unwatched, rating, tag, or recently added.
- Signs people in with built-in usernames and passwords (the first visitor creates the admin, no other service needed) or with any [auth plugin](docs/auth-plugins.md), such as OpenID Connect.
- Manages downloads from its own workspace: qBittorrent torrents, speeds and settings, a protected in-progress folder no scan ever touches, and move rules that put finished torrents into libraries while they keep seeding ([Downloads](docs/downloads.md)).
- Extends through [plugins](docs/plugin-api.md): metadata providers and sign-in methods that ReelStash installs, runs and supervises itself.

## Quick install

On a Linux or macOS machine or server with Docker, one command sets up ReelStash with PostgreSQL, the media and admin apps and, optionally, qBittorrent for Downloads:

```bash
wget -qO- https://raw.githubusercontent.com/Sweyam-in/reelstash/main/scripts/install.sh | sh
```

Details, options and updating: [Quick install](docs/install.md).

## Quick start for development

Prerequisites: Docker, Go **1.26.5** (as specified in `backend/go.mod`), and Node.js **22** with npm (the version used by the UI Docker build).

```bash
npm ci --prefix app
npm ci --prefix admin
scripts/dev.sh
```

Open <http://localhost:3000> for the app and <http://localhost:3001/admin> for the admin. The launcher reads `.env.local` (created from `.env.local.example` on first run), starts PostgreSQL in a Docker container on port `5432` (or uses a PostgreSQL already listening there), the API on `8902`, and Vite on `3000`. With its default local settings, the emergency login password is `admin`. Change `EMERGENCY_LOCAL_PASSWORD` for any shared development machine. The Postgres container remains running after you stop the launcher.

For a Docker-only local stack, see [Getting started](docs/getting-started.md). Set `MEDIA_ROOT` in `.env.local` and start it with `scripts/dev-docker.sh`.

## Documentation

Start at the [documentation index](docs/README.md), or go directly to:

| Guide | Covers |
| --- | --- |
| [Quick install](docs/install.md) | One-command server setup, with optional qBittorrent |
| [Plugin API](docs/plugin-api.md) | Write a metadata provider or sign-in plugin |
| [Getting started](docs/getting-started.md) | Local setup, first library, first scan |
| [Architecture](docs/architecture.md) | Components, data flow, storage, jobs, authentication |
| [Configuration](docs/configuration.md) | Environment variables and defaults |
| [User guide](docs/user-guide.md) | Library, playback, Admin, Files workflows |
| [HTTP API](docs/api.md) | Route groups, authentication, API conventions |
| [Operations](docs/operations.md) | Deployment, backups, health, troubleshooting |
| [Development](docs/development.md) | Repository layout, tests, change workflow |

The [test guide](TESTING.md) has more on running the tests.

## Repository layout

```text
backend/       Go API, workers, providers, embedded SQL migrations
app/              reelstash-app: the media library (React/Vite, own tests and image)
admin/            reelstash-admin: Admin, Files and Downloads (React/Vite, own tests and image)
docs/             Product and developer documentation
scripts/          dev.sh (native run), dev-docker.sh (Compose run), test.sh
.env.local.example  Local settings template (copy to .env.local)
compose.yml       Basic local Compose stack
scripts/install.sh        One-command installer for a full stack (see docs/install.md)
scripts/dev.sh           Local run: native Go + Vite (Docker for Postgres only)
```

## Tests

```bash
scripts/test.sh
scripts/test.sh backend
scripts/test.sh app   # or admin, or ui for both
```

Database integration tests need a disposable PostgreSQL database. The test script uses `reelstash_test` inside the development container when it is running. See [TESTING.md](TESTING.md).

## Security and deployment

The emergency local login in `scripts/dev.sh` is for local development. For a shared deployment, create the admin account before exposing the port (or set `REELSTASH_ADMIN_USERNAME`/`REELSTASH_ADMIN_PASSWORD`), or install a sign-in plugin and register its callback URLs; use a strong database password, and leave `EMERGENCY_LOCAL_AUTH=false` unless the fallback is needed. The basic `compose.yml` exposes both the UI and API on host ports, so review network exposure before using it beyond a trusted development machine. See [Configuration](docs/configuration.md) and [Operations](docs/operations.md).

## About

ReelStash is made by [Sweyam](https://sweyam.com) and released under the GNU General Public License v3 (see [LICENSE](LICENSE)). Product page: [sweyam.com/products/reelstash](https://sweyam.com/products/reelstash). Source and releases: [github.com/Sweyam-in/reelstash](https://github.com/Sweyam-in/reelstash).

The version you are running is shown under **Admin → System → About ReelStash**, and in the `version` field of `GET /api/admin/health` (set at build time with `--build-arg VERSION=…`).
