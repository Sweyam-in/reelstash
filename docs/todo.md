# Feature TODO

Ideas and planned work for ReelStash, grouped by area. Check items off as they ship.
Last checked against the code on 2026-11-26.

## Suggested next

- [ ] Chromecast sender (Google Cast SDK) for desktop Chrome
- [ ] Schedule more job kinds (metadata matching, thumbnail repair)
- [ ] Demo mode with sample content

## Architecture

- [x] **Separate media and admin apps and servers.** `media/` and `admin/` are independent web apps with their own images. The API runs as `cmd/media-server` (browsing, playback, sign-in; no jobs) and `cmd/admin-server` (admin API and every background job), sharing PostgreSQL; `cmd/server` still runs both for local and SQLite installs. See `docs/architecture.md`.
  - [ ] Horizontal scaling of the media server (several replicas behind a load balancer). It is stateless apart from the HLS cache directory, which replicas would need to share.

## High value, fits the existing architecture

- [x] **Continue Watching and Next Up.** Home rows with the resume position ("S1E3 · Resume at 34:12") and the next episode of every show in progress, plus an "Up next" countdown between episodes.
- [x] **Subtitle support.** Sidecar `.srt`/`.vtt`/`.ass` files and embedded text tracks from ffprobe, served as WebVTT to the player.
  - [x] OpenSubtitles.com search and download from a title's settings, saved as a sidecar file next to the video.
  - [x] Admin view of each title's tracks, a "has subtitles" filter, and a job that reads tracks for the whole library.
  - [x] Burn in picture-based subtitles (PGS, VobSub), which browsers cannot show (HLS `burn=`).
- [x] **Multi-audio and track selection.** Language switching in the player (remux when the codecs allow, transcode otherwise) and per-user audio and subtitle defaults.
- [x] **Collections (auto and manual).** Smart playlists built from saved filters (unwatched, rating, tag, studio, year, added in the last N days, ...) inside the existing playlists.

## Admin and operations

- [x] **Storage health.** Per-disk usage and history (Admin → Storage), offline disks, and Housekeeping for unused images, stale transcode caches and unfinished files (analyze first, then clean up).
- [x] **Duplicate resolution.** Side-by-side groups with "keep this one", moving the others to a restorable trash (Admin → Duplicates).
- [x] **Scheduled jobs and file watching.** Cron scans, duplicate checks and housekeeping, and inotify-triggered scans; failures notify admins and subscribed webhooks.
  - [ ] Schedule more job kinds (metadata matching, thumbnail repair).
  - [x] Polling fallback for network shares, where inotify sees nothing.
- [x] **Backup and restore of user data.** JSON export/import of favorites, ratings, history, Watch later and playlists (Settings → Your library data).

## Open-source readiness

- [x] **Provider plugin system.** HTTP provider contract (manifest, search, details; optional series), connected in Admin → Providers. See `docs/provider-plugins.md`.
  - [x] Per-plugin enable/disable switch and request logging.
  - [x] Bundle plugins as scripts the server runs itself. Install, configure, switch off and remove them under Admin → Plugins. See `docs/plugin-bundles.md`.
- [x] **Auth plugins and built-in accounts.** Usernames and passwords with no identity provider (first visitor creates the admin), and sign-in plugins installed as bundles. See `docs/auth-plugins.md`.
  - [ ] Plugin kinds beyond providers and sign-in (subtitle sources, notification channels).
  - [x] Update plugins: per-plugin update source, check and verified update from the Plugins page.
  - [x] Plugin catalog: save one or more `index.json` addresses under Admin → Plugins → Catalog, browse what they list and install with a sha256 check. The catalog becomes the plugin's update source.
- [x] **First-run setup wizard.** Library type, folder check, optional provider key, first scan with progress.
  - [x] Create the first admin user (the sign-in screen offers it on a fresh install).
  - [ ] Offer installing a sign-in plugin from the wizard.
- [ ] **Demo mode with sample content.** Seed a few CC-licensed clips for screenshots and CI end-to-end tests.

## Media app

- [x] Player keyboard shortcuts: J/K/L, arrows (seek and volume), 0–9, Home/End, frame-step, speed, markers, captions, and a `?` list of them all.
- [x] Picture-in-picture and playback speed.
- [x] Chapter markers and skip intro (from markers named intro, opening or recap).
  - [ ] Detect intros automatically (audio fingerprinting across episodes).
- [x] AirPlay and the browser's Remote Playback (Chrome on Android).
  - [ ] Google Cast SDK for desktop Chrome.
- [x] Installable app: web manifest and a service worker that caches the app shell (never the API or video).
  - [ ] "Download for later" offline playback.
- [x] Per-user view preferences: title artwork (posters or thumbnails), grid size, and each list remembers its sort and filters.
- [x] Stats: hours watched, plays, streaks, top performers, studios and categories (Activity).
  - [ ] Year in review.
- [x] Pages load on demand; hls.js loads only for transcoded playback.

## Known follow-ups

- [x] NFOs carry season/episode numbers.
- [ ] `TestPerformerPhotoGallery` fails against PostgreSQL (the list comes back empty); it failed before the media/admin split too.
- [ ] Admin's `navigation.test.js` expects at most 18 sidebar entries; the sidebar has grown past that.
