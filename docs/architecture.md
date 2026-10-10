# Architecture

## System overview

```mermaid
flowchart LR
  Browser[Browser: Media, Admin, Files] --> Web[Vite in development / nginx in Docker]
  Web -->|/api, /auth, /healthz| API[Go HTTP API]
  API --> DB[(PostgreSQL)]
  API --> Jobs[Background job runners]
  Jobs --> FS[(Mounted media)]
  Jobs --> Assets[(Thumbnails, posters, sprites, previews)]
  Jobs --> Providers[Metadata providers]
  API --> FS
  API --> Assets
  API --> Providers
  API --> AuthPlugin[Sign-in plugins, such as OpenID Connect]
  API --> Bundles[Plugin bundles: provider and sign-in services on 127.0.0.1]
  API --> Events[SSE events and notifications]
  Events --> Browser
```

The Go process in `backend/cmd/server/main.go` connects to PostgreSQL, applies embedded SQL migrations, sets up authentication and providers, registers job handlers, starts the HTTP server, and samples storage capacity. It shuts down HTTP and workers gracefully on SIGTERM or Ctrl+C.

## Frontend and HTTP boundary

`ui/` builds four HTML entries: `index.html` for the media library, `admin.html` for Admin, `files.html` for Files and `downloads.html` for Downloads. In development, Vite serves them from one origin. The production Dockerfile has `media` and `admin` targets so the media UI and Admin + Files can run on separate origins. nginx serves the static apps and forwards `/api`, `/auth`, and `/healthz` to the Go service. It disables proxy buffering for server-sent events and direct media streams.

`ui/src/api.js` centralizes frontend API calls. `ui/src/libraryContext.jsx` tracks the selected library. The Go router (`internal/httpapi/router.go`) is the route and authorization boundary; hiding an Admin link in React never grants or removes permissions.

## Backend packages

| Package | Responsibility |
| --- | --- |
| `httpapi` | Routes, request validation, authorization, streaming, and admin endpoints |
| `auth` | Password accounts, sessions, and identity state |
| `authplugin` | Client and registry for sign-in plugins |
| `plugins` | Installs plugin bundles and supervises their processes ([Plugin bundles](plugin-bundles.md)) |
| `database` | PostgreSQL pool and embedded migrations |
| `jobs` | Persisted queue, worker concurrency, cancellation, resumption |
| `scanner` | Filesystem discovery, probing, NFO import, hashes, thumbnails, sprites, previews, duplicates |
| `namer`, `provider`, `tmdb`, `titlematch` | Metadata matching, the provider interface, the built-in TMDB provider, and the title helpers ReelStash itself needs |
| `organizer`, `rename` | Rule-driven organization and file renaming |
| `events`, `notify` | SSE job updates, notifications, webhooks |
| `extprovider` | Adapts an external HTTP service to the provider interface (plugins) |
| `schedule` | Cron-driven scans and duplicate checks, and inotify file watching that triggers scans |
| `qbit`, `downloads` | qBittorrent WebUI client; the Downloads service that polls it, protects the in-progress folder and runs move rules ([Downloads](downloads.md)) |
| `pathguard` | Which paths scans, the watcher, organizer, housekeeping and Files must leave alone: locked folders and torrent data in use |
| `lang` | Normalises language tags so audio and subtitle tracks match a user's language preference |
| `recommend`, `diskinfo` | Recommendations and storage capacity data |

## Scan and matching flow

```mermaid
flowchart LR
  Root[Storage location] --> Walk[Discover supported files]
  Walk --> Compare[Compare with media_file rows]
  Compare --> Probe[ffprobe, NFO, partial hash]
  Probe --> Persist[(media_file + media_item)]
  Persist --> Art[Thumbnail and preview jobs]
  Persist --> Match[Optional provider matching]
  Match --> Review[Review and confirm]
```

A scan is incremental: it discovers files, compares them with existing records, probes new or changed files, and updates database records. Thumbnail and related asset work uses a separate worker pool, so a long scan cannot occupy every thumbnail worker. Exact duplicate detection is a separate job because it must read full file contents. Scan progress is stored in `scan_job`; general job state is stored in `background_job`.

The `jobs` package persists pending work before queueing it. On startup, the runners resume pending jobs. On graceful shutdown, in-flight jobs are returned to pending so they can resume; a user cancellation is recorded as cancelled. The main worker pool runs scan, organizer, matching, and maintenance work; the thumbnail pool handles thumbnail, sprite, and preview generation.

## Automation

`schedule.Scheduler` wakes at the start of every minute and runs each enabled `scan_schedule` row whose cron expression matches, queueing the same `SCAN` or `DUPLICATE_SCAN` jobs an admin would. `schedule.Watcher` keeps one `fsnotify` watcher per storage location with `watch_enabled`, adds watches for new folders, ignores `.nfo` files (ReelStash writes those itself), and queues a scan once 45 seconds pass without a relevant change. Both go through `schedule.EnqueueScan`, which refuses a second scan of a disk that is scanning or being organized. Failures reuse the notification path of `jobs.SetFinishHook`; a schedule or watcher that cannot start raises an admin notification directly.

## Audio and subtitle tracks

`ffprobe` output is stored in `media_track` when a file is scanned, and `media_file.sidecar_subtitles` counts the subtitle files beside it. The `TRACK_SCAN` job (queued after every scan) reads the tracks of files that have none yet and recounts subtitle files for every video; files are otherwise filled in the first time their tracks are requested. `GET /api/media/{id}/tracks` adds sidecar subtitle files found next to the video. Text subtitles are served as WebVTT, converted with `ffmpeg` (or in Go for `.srt`) and cached. `?audio=N` selects an audio stream: a stream-copy remux for direct play, or `-map 0:a:N` in the HLS transcode, each cached separately.

## Data model

PostgreSQL is the source of truth. The base migration defines users, storage locations, media items and files, people, studios, series, tags, collections, playback state, duplicate groups, and jobs. Later migrations add sessions, provider links, player features, notifications, library roots and access, scan rules, and per-user favorites. `library_root` maps storage paths to libraries; `library_access` controls which libraries a user can browse.

Media files and generated assets are separate from database rows. Backups should include the database and asset volumes. The original media mount is managed separately and should be backed up according to its own storage policy. Migrations are embedded in the Go binary and applied in filename order at startup; a failed migration stops startup.

## Authentication and authorization

Sign-in has four front doors that all end in the same server-side session, identified by an HTTP-only cookie: ReelStash's own username/password accounts (`/auth/native-login`, and `/auth/setup` for the first admin), sign-in plugins (`/auth/plugin/{id}/...`, plus any callback address a plugin declares, such as `/auth/callback`, and `/auth/login` for the first redirect plugin), and the optional emergency password. `/auth/config` tells the sign-in screen which are available; `/auth/logout` ends the session. Every `/api/*` request requires a session. Admin routes require an Admin role; library scope restricts media and file access. A selected library is sent through `X-Library` or `?library=` for direct browser URLs. The optional app or library PIN adds a lock layer.

See [HTTP API](api.md) for route groups and [Configuration](configuration.md) for auth settings.
