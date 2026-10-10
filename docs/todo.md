# Feature TODO

Ideas and planned work for ReelStash, grouped by area. Check items off as they ship.

## Suggested next

- [ ] Storage health dashboard
- [ ] Duplicate resolution UI

## High value, fits the existing architecture

- [x] **Continue Watching and Next Up.** Home rows with the resume position ("S1E3 · Resume at 34:12") and the next episode of every show in progress, plus an "Up next" countdown between episodes.
- [x] **Subtitle support.** Sidecar `.srt`/`.vtt`/`.ass` files and embedded text tracks from ffprobe, served as WebVTT to the player.
  - [x] OpenSubtitles.com search and download from a title's settings, saved as a sidecar file next to the video.
  - [x] Admin view of each title's tracks, a "has subtitles" filter, and a job that reads tracks for the whole library.
  - [ ] Burn in picture-based subtitles (PGS, VobSub), which browsers cannot show.
- [x] **Multi-audio and track selection.** Language switching in the player (remux when the codecs allow, transcode otherwise) and per-user audio and subtitle defaults.
- [x] **Collections (auto and manual).** Smart playlists built from saved filters (unwatched, rating, tag, studio, year, added in the last N days, ...) inside the existing playlists.

## Admin and operations

- [ ] **Storage health dashboard.** Per-disk usage, damaged files, missing files after a disk unmount, transcode cache size with a purge button.
- [ ] **Duplicate resolution UI.** Side-by-side compare (resolution, bitrate, size) with "keep best, move the rest to trash". Needs a reversible soft-delete trash.
- [x] **Scheduled jobs and file watching.** Cron scans and duplicate checks, and inotify-triggered scans; failures notify admins and subscribed webhooks.
  - [ ] Schedule more job kinds (metadata matching, thumbnail repair).
  - [ ] Polling fallback for network shares, where inotify sees nothing.
- [ ] **Backup and restore of user data.** JSON export/import of favorites, ratings, history and playlists.

## Open-source readiness

- [x] **Provider plugin system.** HTTP provider contract (manifest, search, details; optional series), connected in Admin → Providers. See `docs/provider-plugins.md`.
  - [x] Per-plugin enable/disable switch and request logging.
  - [x] Bundle plugins as scripts the server runs itself. Install, configure, switch off and remove them under Admin → Plugins. See `docs/plugin-bundles.md`.
- [x] **Auth plugins and built-in accounts.** Usernames and passwords with no identity provider (first visitor creates the admin), and sign-in plugins installed as bundles. See `docs/auth-plugins.md`.
  - [ ] Plugin kinds beyond providers and sign-in (subtitle sources, notification channels).
  - [x] Update plugins: per-plugin update source, check and verified update from the Plugins page.
  - [ ] Plugin store: browse and install bundles from a list (the `reelstash_plugins` repo already writes `dist/index.json` with every released version and its sha256).
- [x] **First-run setup wizard.** Library type, folder check, optional provider key, first scan with progress.
  - [x] Create the first admin user (the sign-in screen offers it on a fresh install).
  - [ ] Offer installing a sign-in plugin from the wizard.
- [ ] **Demo mode with sample content.** Seed a few CC-licensed clips for screenshots and CI end-to-end tests.

## Smaller polish

- [ ] Player keyboard shortcuts (J/K/L, frame-step, speed) and picture-in-picture.
- [ ] Chapter markers and skip-intro detection.
- [ ] Chromecast and AirPlay support.
- [ ] PWA or mobile layout with offline "download for later".
- [ ] Per-user view preferences (grid density, sort defaults) on the settings page.
- [ ] Stats page: hours watched, top performers or genres, year in review.

## Known follow-ups

- [ ] NFOs don't carry season/episode numbers.
