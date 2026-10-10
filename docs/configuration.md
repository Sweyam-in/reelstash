# Configuration

The Go server reads environment variables in `backend/internal/config/config.go`. Empty values use the listed defaults unless the setting is explicitly described as optional. The installer ([Quick install](install.md)) writes a `.env` with the Compose-level variables; keep populated environment files out of Git.

## Core settings

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | `postgres://reelstash:reelstash@localhost:5432/reelstash?sslmode=disable` | PostgreSQL connection string |
| `REELSTASH_ADDR` | `:8902` | API listen address |
| `REELSTASH_PUBLIC_ORIGIN` | `http://localhost:8902` | Configured public origin; currently loaded but not otherwise referenced by the Go server |
| `REELSTASH_DB_MAX_CONNS` | pgx default (`0` means unset) | PostgreSQL pool size override |
| `REELSTASH_THUMBNAIL_DIR` | `/app-data/thumbnails` | Thumbnail output directory |
| `REELSTASH_POSTER_DIR` | `/app-data/posters` | Poster and backdrop output directory |
| `REELSTASH_SPRITE_DIR` | sibling `sprites` directory | Scrub sprite output directory |
| `REELSTASH_PREVIEW_DIR` | sibling `previews` directory | Hover preview output directory |
| `REELSTASH_SUBTITLE_DIR` | sibling `subtitles` directory | Cache of subtitles converted to WebVTT. Give it a volume so deploys keep it |
| `REELSTASH_RENAME_TEMPLATE` | `{studio} - {date} - {title} [{resolution}]` | Fallback filename template |

Mount media paths so they are visible inside the backend container and writable only when Admin file operations or organization need to change them. The asset directories must be writable by the API process.

## Authentication

ReelStash works with **no identity provider at all**: on a fresh install the sign-in screen asks the first visitor to create the admin account, and admins add other users under **Admin → Users & access**. Accounts are a username and a password kept (salted and hashed with PBKDF2-SHA256) by ReelStash. Repeated wrong passwords are slowed per address and username.

To skip the first-visitor step, set `REELSTASH_ADMIN_USERNAME` and `REELSTASH_ADMIN_PASSWORD`: at start, ReelStash creates that admin if the username does not exist. An existing account is never changed, so a password changed in the app sticks. Remove the password from the environment afterwards.

Other ways in are added next to passwords by installing an [auth plugin](auth-plugins.md), for example an OpenID Connect provider. The sign-in screen offers every method that is available. First-run admin creation is only offered while no admin exists and no sign-in plugin or emergency login is configured.

| Variable | Default | Purpose |
| --- | --- | --- |
| `REELSTASH_ADMIN_USERNAME` / `REELSTASH_ADMIN_PASSWORD` | empty | Create this native admin at start when the username is new |
| `EMERGENCY_LOCAL_AUTH` | `false` | Enables a single-password fallback login |
| `EMERGENCY_LOCAL_PASSWORD` | `changeme` | Fallback password; set explicitly before enabling it |

There are no identity-provider variables: An OpenID Connect provider is configured in its plugin's settings under **Admin → Plugins**. The local launcher deliberately enables the emergency login for development.

## Plugin bundles

| Variable | Default | Purpose |
| --- | --- | --- |
| `REELSTASH_PLUGIN_BUNDLES` | `true` | `false` switches off installing and running [plugin bundles](plugin-bundles.md) |
| `REELSTASH_PLUGIN_DIR` | sibling `plugins` directory of the thumbnail directory (`/app-data/plugins`) | Where bundles are installed; keep it on a volume |

## Multiple UI origins

`REELSTASH_MEDIA_URLS` and `REELSTASH_ADMIN_URLS` are comma-separated lists paired by position. Each media origin maps to one Admin + Files origin. The UI reads the matching pair from `/auth/config` to build cross-app links. Leave both empty when all three apps share one origin, as with Vite development. When using separate origins, include all public and LAN pairs and register each corresponding sign-in callback with the identity provider.

## Workers, streaming, and providers

| Variable | Default | Purpose |
| --- | --- | --- |
| `REELSTASH_SCAN_WORKERS` | `4` | General background-job concurrency |
| `REELSTASH_SCAN_FILE_WORKERS` | `4` | Per-scan concurrent file processing |
| `REELSTASH_THUMBNAIL_WORKERS` | `2` | Thumbnail/sprite/preview job concurrency |
| `TZ` | `UTC` | Time zone for cron schedules (for example `Asia/Kolkata`); passed through by the Compose files |
| `REELSTASH_HLS_SEGMENT_SECONDS` | built-in 6 seconds (`0`) | HLS segment length |
| `REELSTASH_HLS_WORKERS` | built-in CPU-based limit (`0`) | Concurrent HLS ffmpeg transcodes |

Metadata providers can be configured through **Admin → Metadata → Providers**. The process bootstraps provider configuration from PostgreSQL. ffmpeg and ffprobe are installed in the backend Docker image. If running the Go binary directly, install them on the host.

## Downloads

| Variable | Default | Meaning |
| --- | --- | --- |
| `REELSTASH_QBIT_URL` | empty | qBittorrent's WebUI address. Settings saved under **Downloads → Settings** override it. |
| `REELSTASH_QBIT_USERNAME`, `REELSTASH_QBIT_PASSWORD` | empty | qBittorrent sign-in, overridable in the app. |

The Compose `downloads` profile and its variables are described in [Downloads](downloads.md).

## Compose-specific variables

The Compose files also use substitution variables that are **not** read by Go. Examples include `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `MEDIA_ROOT`, image names and tags, and published LAN ports. Read the Compose file you actually deploy:

- `compose.yml`: local source builds and development defaults.
- The installer's stack (`scripts/install.sh --print-compose`): published images, separate media and Admin apps, named volumes, optional qBittorrent. Its variables (`REELSTASH_IMAGE_TAG`, `MEDIA_ROOT`, `POSTGRES_PASSWORD`, ...) live in the `.env` it writes. See [Quick install](install.md) and [Operations](operations.md).
