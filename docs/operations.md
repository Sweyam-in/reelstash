# Operations

## Choose a deployment path

| Stack | Use | Notes |
| --- | --- | --- |
| `scripts/dev.sh` | Local coding | Runs Go and Vite on the host; Docker is used for PostgreSQL |
| `compose.yml` | Simple local container setup | Builds API and media UI, bind-mounts `./data`, publishes `8902` and `3091` |
| `scripts/install.sh` | Running ReelStash on a server or home machine | One command: PostgreSQL, API, media app, Admin and optional qBittorrent from published images, with generated passwords. See [Quick install](install.md) |

`scripts/install.sh --print-compose` prints the Compose file the installer writes, for anyone who prefers to manage it by hand. The images are `reelstash-api` (Go API), `reelstash-ui` (media app) and `reelstash-admin` (Admin, Files, Downloads).

## Preflight for a shared host

1. Set a strong PostgreSQL password and keep populated environment files out of Git.
2. Mount the intended media root and persistent generated-asset directories. Verify the API container can read the paths configured in Storage.
3. Decide how people sign in. Either use ReelStash's own accounts (create the admin right away, or set `REELSTASH_ADMIN_USERNAME`/`REELSTASH_ADMIN_PASSWORD`), or install an [auth plugin](auth-plugins.md) and register its callback URLs with the provider. Disable emergency local auth unless needed for recovery.
4. Put the UI behind the intended HTTPS proxy or tunnel. The root Compose example publishes the Go API directly on the host too; restrict that exposure if users should only reach the UI proxy.
5. Preserve database and asset volumes across image replacement. Account for original media storage separately.

## Health and logs

`GET /healthz` returns 503 when PostgreSQL cannot be reached. When the database answers it returns 200 with `status` `ok` or `degraded`. `degraded` means `ffmpeg` or `ffprobe` is missing, or a storage location is offline or not readable. It does not prove metadata providers are healthy. **Admin → Health**, **Admin → Storage**, and **Admin → Jobs** provide more application detail.

```bash
docker compose ps
docker compose logs -f reelstash ui postgres
curl -fsS http://localhost:8902/healthz
```

Run those commands against the Compose file and directory you deployed. If you run a log viewer next to the stack, keep it off public ingress.

## Backups and restore

A usable backup includes PostgreSQL **and** the generated assets: thumbnails, posters, sprites, and previews. The original media mount is separate. Back up that mount using your normal storage backup process. With the installer's stack the data is in Docker volumes (`postgres_data`, `thumbnail_data`, `poster_data`, `sprite_data`, `preview_data`, `subtitle_data`, `plugin_data`).

For a generic Compose stack, a logical database dump can be taken with `pg_dump` from the Postgres container:

```bash
docker compose exec -T postgres pg_dump -U reelstash -d reelstash -Fc > reelstash.dump
```

Use the actual `POSTGRES_USER` and `POSTGRES_DB` values if different. Copy the asset directories or named volumes as well. To restore, stop application writers, restore the database into a compatible PostgreSQL instance, restore the assets at their configured paths, and then start the API. The API applies embedded migrations on startup; test restores on a separate host before treating backups as complete. Do not restore a production dump into the test database used by `scripts/test.sh`.

## Automation

- **Time zone.** Cron schedules use the API process's local time. Containers default to UTC; set `TZ` (for example `TZ=Asia/Kolkata`) on the API service to schedule in local time. **Admin → Automation** shows the zone in use.
- **File watching** uses inotify, one watch per folder. A very large library can exceed the host's limit (`fs.inotify.max_user_watches`); the Automation page then shows "Cannot watch" and admins get a notification. Raise the limit on the Docker host (for example `sysctl fs.inotify.max_user_watches=524288`). inotify does not report changes made by other machines to NFS/SMB shares: use a schedule there.
- **Caches.** Extracted subtitles are cached under the data directory's `subtitles` folder, alongside `hls` (transcode segments per quality and audio track) and `remux`. the **System health** section of the admin dashboard lists their sizes. They can be deleted at any time and are rebuilt on demand.

- **Downloads.** The optional `downloads` Compose profile runs qBittorrent on the same media mount ([Downloads](downloads.md)). Back up `./data/qbittorrent` with the rest of `./data`; it holds qBittorrent's resume data.

## Updating

For the local stack, rebuild and restart with `docker compose up --build -d`. Use `scripts/install.sh` to upgrade a standard install. Take a database and asset backup before changing versions. Migrations run at API startup and are ordered by filename. Review migration SQL before rolling back an image, because a code rollback does not reverse schema changes.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Login works on one host but not another | Register every host's `/auth/callback` with the OIDC client and list them in the auth plugin's **Registered redirect URIs**. Check paired `REELSTASH_MEDIA_URLS` and `REELSTASH_ADMIN_URLS`. |
| API returns 401 | Inspect `/auth/config`, the session cookie, and auth configuration. On a fresh install, open the app and create the admin account. |
| Locked out of password sign-in | Too many wrong passwords are refused for 15 minutes per address and username. Wait, or restart ReelStash to clear it. An admin can reset any password under Users & access; with no admin able to sign in, set `REELSTASH_ADMIN_USERNAME`/`REELSTASH_ADMIN_PASSWORD` to a new username, or enable `EMERGENCY_LOCAL_AUTH` temporarily. |
| A plugin shows "Not working" | Open its card under Admin → Plugins and press **Show log**: the service's own output and why ReelStash stopped retrying. Check the program (`python3`) exists in the image and the settings are filled in, then **Restart**. |
| A plugin never starts | Check `REELSTASH_PLUGIN_BUNDLES` is not `false` and that `REELSTASH_PLUGIN_DIR` is on a writable volume. |
| API returns 403 | Check Admin role or library grants. |
| Media folder is empty | Check the path **inside the API container**, library roots, scan rules, mount permissions, and scan job status. |
| Scan or thumbnail job fails | Inspect **Admin → Jobs** and API logs; ensure `ffprobe`/`ffmpeg` are available and asset directories are writable. |
| Playback stalls | Check the media mount, direct stream range requests, HLS worker load, and reverse proxy buffering. The included nginx config has stream-specific settings. |
| New files do not appear by themselves | Switch on file watching (or add a schedule) under **Admin → Automation**; check its status there. Watching cannot see writes made by other machines to a network share. |
| A subtitle or audio track is missing from the player | Picture-based subtitles (PGS, VobSub) are not offered. Sidecar subtitles must start with the video's file name. Tracks of files scanned before this feature are read the first time the title is opened. |
| No live job updates | Confirm `/api/events` is proxied without buffering and with a long read timeout. |
| Database startup fails | Confirm `DATABASE_URL`, database readiness, and the named migration reported in API logs. |

On a network mount, a blocked filesystem call can delay scanning even when the scan's stall detection reports failure. Resolve the mount itself before repeatedly retrying scans.
