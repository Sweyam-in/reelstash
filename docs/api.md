# HTTP API

The API listens on `REELSTASH_ADDR` (default port `8902`). Browser deployments normally call it through the UI origin's nginx or Vite proxy. The route registry in `backend/internal/httpapi/router.go` is the exhaustive source of methods and paths; this page describes the public contract and major groups.

## Authentication and request conventions

- `GET /healthz` is public and checks server/database health.
- `/auth/config`, `/auth/login`, `/auth/callback`, `/auth/local-login`, and `/auth/logout` handle browser sign-in and sessions.
- Every `/api/*` route requires a valid session: the HTTP-only session cookie (browsers) or `Authorization: Bearer <token>` (apps). Admin routes also require the Admin role.

### App sign-in (device sessions)

Apps such as the Android app hold a bearer token, a *device session* that lasts 180 days and slides forward while the app is used.

- `GET /auth/server-info` (public) returns `{product: "reelstash", name, version, apiVersion, passwordLogin, pairing, plugins}`. An app checks an address with it before signing in. `name` comes from `REELSTASH_SERVER_NAME`, and `version` from the build.
- `POST /auth/token` with `{username, password, deviceName, platform}` signs in with a ReelStash password and returns `{token, expiresAt, user}`. It is throttled like the web sign-in.
- Pairing works with every sign-in method, including plugins. `POST /auth/device/start` with `{deviceName, platform}` returns `{deviceCode, userCode, verificationUrl, expiresIn, interval}`. The app shows `userCode` (for example `MG6Z-G4NH`) and polls `POST /auth/device/poll` with `{deviceCode}`: `202` while waiting, `404` once expired, and `{token, expiresAt, user}` once approved. A signed-in person approves the code on the media app's `/link` page or under Settings → Devices, which call `GET /api/devices/pair?code=` (which device it is) and `POST /api/devices/pair` with `{code}`.
- `GET /api/devices` lists the account's signed-in apps (`current` marks the caller), and `DELETE /api/devices/{id}` signs one out. `POST /auth/logout` with a bearer token ends that device session.
- With a PIN lock, apps send the unlock token in `X-Reelstash-Unlock` instead of the `rs_unlock` cookie. `POST /api/applock/verify` returns it as `unlockToken`, and any response may carry a refreshed one in the same header.
- `GET /api/media/{id}/stream?raw=1` skips the automatic `.mkv` to `.mp4` remux, for players that read the original container themselves.
- Select a library with `X-Library: <id>`. For direct media URLs opened by the browser, use `?library=<id>`. Access is checked on the server.
- JSON endpoints use JSON request and response bodies. Error responses include an error message; callers should also inspect HTTP status codes.
- `GET /api/events` is a server-sent event stream, not a JSON document.

For an authenticated browser session, an example read is:

```bash
curl -b cookies.txt -H 'X-Library: 1' http://localhost:8902/api/media/
```

The UI's `ui/src/api.js` shows how it sends requests and handles responses. API route authorization is covered by backend tests in `internal/httpapi/authz_test.go`.

## Route map

| Prefix | Responsibility | Access |
| --- | --- | --- |
| `/api/me`, `/api/applock` | Current user and optional PIN lock | Signed in |
| `/api/libraries`, `/api/providers` | Available libraries and providers | Signed in |
| `/api/media`, `/api/search` | Browse titles, details, metadata, artwork, streams | Library scoped; shared metadata edits require Admin |
| `/api/performers`, `/api/studios`, `/api/series` | People and grouping metadata | Library scoped; write operations require Admin |
| `/api/categories`, `/api/tags`, `/api/collections` | Organization and playlists (manual, and smart playlists whose `rules` filter the library) | Signed in, with per-route ownership and Admin checks |
| `/api/playback`, `/api/watch-later` | Playback state, watch sessions, `continue` and `next-up` rows | Signed in and media scoped |
| `/api/preferences` | The user's audio and subtitle language defaults | Signed in |
| `/api/notifications`, `/api/events`, `/api/activity` | Notifications, live updates, watch activity | Signed in |
| `/api/recommendations/home` | Home recommendations | Signed in |
| `/api/files` | List, download, and read NFO entries | Signed in and library scoped |
| `/api/downloads/*` | qBittorrent: torrents, categories, settings, move rules ([Downloads](downloads.md#api)) | Admin |
| `/api/admin/*` | Configuration, storage, jobs, organizer, files, metadata | Admin |

## Playback and generated assets

`/api/media/{id}/tracks` lists a file's audio tracks and text subtitles (embedded and sidecar); `/api/media/{id}/subtitles/{track}.vtt` serves one as WebVTT (`e<n>` for the nth embedded subtitle, `x<hash>` for a sidecar file). `?audio=N` on `/stream`, `/hls/index.m3u8` and `/hls/seg/{n}.ts` selects the Nth audio track (0 is the first). Admins can also `POST /api/media/{id}/tracks/refresh` (read the tracks again), `GET /api/media/{id}/subtitles/search?q=&languages=en,fr` (OpenSubtitles, by the file's hash and the text), `POST /api/media/{id}/subtitles` with `{fileId, language, hearingImpaired, foreignPartsOnly}` (download and save beside the video) and `DELETE /api/media/{id}/subtitles/x<hash>` (remove a subtitle file beside the video). OpenSubtitles settings live at `/api/admin/subtitle-provider` (`GET`, `PUT`, `DELETE`, `POST …/test`); `/api/admin/videos?tracks=subtitles|nosubtitles|multiaudio|unread` filters the admin video list. `GET /api/media?reels=true` lists the selected library's reels: titles whose played file is portrait (`media_file.portrait`, computed from width, height and rotation); `sort=shuffle&seed=<any text>` gives a random order that stays the same while paging with that seed. Admins set a title's override with `PUT /api/media/{id}/reel` and `{"override": true|false|null}` (null: automatic); title details carry `reel`, `reelOverride` and `reelDetected`. `/api/media/{id}/next` returns the next title in the same series. `/api/media/{id}/stream` serves direct playback. `/api/media/{id}/playback-info` provides playback choices; `/api/media/{id}/hls/index.m3u8` and `/api/media/{id}/hls/seg/{n}.ts` provide HLS. Artwork, frame, sprite, preview, and marker routes also live under `/api/media/{id}`. Keep the browser session and library selection when requesting those URLs. The nginx config deliberately avoids buffering large direct streams and SSE responses.

## Admin route areas

`/api/admin` includes overview, analytics, health, settings, users (including password accounts and sign-in settings), plugin bundles (`/plugins`, install, settings, restart, log), provider plugin switches and request logs, provider order and matching (`PUT /providers/order` takes `order`, `mode` (`priority`|`best`) and `autoMatch` (`{titles, performers, studios}` booleans; kinds left out are unchanged); `GET /providers` returns the current `autoMatch`), libraries, storage, scan rules, automation (`/automation`, `/schedules` — `jobKind` `SCAN`, `DUPLICATE_SCAN` or `HOUSEKEEPING`, `/libraries/{id}/watch`), housekeeping (`GET /housekeeping` returns the tasks, retention settings, running job, last analysis and last clean-up; `PUT /housekeeping/settings`; `POST /housekeeping/run` with `{tasks, dryRun}`, no tasks meaning the defaults, queues a `HOUSEKEEPING` job), webhooks, videos, organizer, Files mutations, duplicate scans, scans, jobs, rename, review, namer, and metadata synchronization. Users: `GET /users` returns everyone, or with `limit`/`offset` pages on the server (`q`, `role`, `status` = `active`|`never`|`disabled`, `method` = `password`|`sso`, `sort` = `name`|`role`|`login`|`libraries`, `dir`) with `total` and summary `counts`; `GET /users/{id}`; `DELETE /users/{id}`; `GET`/`DELETE /users/{id}/sessions` and `DELETE /users/{id}/sessions/{publicId}` list and revoke sessions (secrets are never returned). The server refuses to disable, demote or delete your own account or the last enabled administrator. `GET /jobs` takes `offset` and returns `total` for the filter. `GET /webhooks/{id}/deliveries` is the latest 50 deliveries.

Audit trail: every successful admin write (any non-GET under `/api/admin`) is recorded with the actor, a derived `action` (`users.update`, `libraries.delete`, …), the target and, when the request has an `X-Admin-Reason` header (500 characters), the reason. Reads are not recorded. `GET /audit` pages it newest first (`limit`, `offset`, `q`, `actor`, `action` prefix, `targetKind`, `targetId`, `from`, `to`) and returns `total`.

Mutating endpoints can enqueue jobs or change files. Use the Admin UI's previews for organizer and rename changes.

Examples of route entry points:

```text
GET  /api/admin/storage
POST /api/admin/libraries/{id}/scan
POST /api/admin/scans/{storageLocationID}
GET  /api/admin/jobs
POST /api/admin/jobs/{id}/cancel
GET  /api/admin/organizer/{libraryID}/preview
POST /api/admin/organizer/{libraryID}/run
POST /api/admin/jobs/run            {"kind": "SCAN", "libraryId": 3}
GET  /api/admin/review
```

For exact payload fields and response shapes, inspect the handler in `backend/internal/httpapi/` and the matching call in `ui/src/api.js`. Keep both synchronized when adding an endpoint; `internal/httpapi/contract_test.go` checks that UI endpoint calls have a corresponding router route.
