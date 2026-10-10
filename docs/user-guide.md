# User guide

## Roles and apps

The **Media** app is for browsing and watching. **Admin** manages storage, libraries, users, metadata, jobs, and organization. **Files** provides a browser for storage locations. **Downloads** (admins only) manages a qBittorrent: see [Downloads](downloads.md). In development they share one origin (`/`, `/admin`, `/files`, `/downloads`); a production deployment can serve Media and Admin + Files + Downloads on paired origins.

All users need a valid session. Library grants determine what a non-admin can see. Admin-only operations are enforced by the API. Some library content can also be protected by a PIN lock.

## First-run setup wizard

A fresh install opens **Admin** on a four-step wizard (also at **Admin → System → Setup wizard**, where it adds another library). Pick the library type, enter the media folder as the API process sees it and check it, optionally add and test a metadata key (TMDB for Entertainment, a provider plugin for Private), and let it create the disk and library and run the first scan. An install that already has a disk and a library is never sent through it.

## Set up libraries

In **Admin → Libraries**, create a library and choose its mode. Entertainment mode presents movies, shows, and cast-oriented navigation. Private mode presents videos, performers, studios, and series-oriented navigation. Add roots that point into registered storage locations, then grant users access to the library in **Admin → Users**. A user can switch among libraries they can access; the selected library narrows listings and search results.

## Add storage and scan

1. Make the media directory available to the API process. For Docker, bind-mount it into the backend container.
2. Add it in **Admin → Storage** using the path visible inside that container.
3. Add the folder to a library in **Admin → Libraries**.
4. Configure **Admin → Scan rules** if particular paths should be skipped. A rule applies to one library or to every library; a pattern with a slash (`Downloads/incomplete`) starts at the library's folder.
5. Scan the library from its card in Libraries, from the Dashboard, or from **Admin → Jobs**. A library scan reads only that library's folders (a subfolder that belongs to another library is skipped). The first time a library scan cannot see a file, it flags that file and keeps the title, its watch history, ratings and playlist entries. A later scan that still cannot see it deletes the file from the library, and a title left with no file goes with it. A library folder that is empty of videos, or that has disappeared, is left alone rather than treated as a deleted library. Folders it can't read and paths a scan rule skips are left alone too. A whole disk can still be scanned from its page under Storage; that scan only flags missing files. **Admin → Jobs** shows running and completed work; a scan can be cancelled there or where it was started.

Scanning indexes supported videos, reads metadata and adjacent NFO files, and schedules generated artwork work. It compares against prior records so later scans focus on changes. Provider matching is optional: files remain indexed even without an API token. Use **Admin → Metadata → Providers** to enable and test provider credentials, and **Admin → Review** to inspect matches needing confirmation. **Automatic matching** on the Providers page decides, separately for titles, people and studios, whether a confident match is applied on its own or kept as a suggestion to review. By default people and studios are linked automatically when exactly one provider result has the same name (or alias), and titles always wait for review. Matches you rejected are never applied again.

## Browse and play

The Media app offers a home page, catalog, search, people and studio browsing, playlists, favorites, watch later, and activity. Entertainment libraries expose movies and shows. Open a title to view details and play it. The API can serve direct streams with byte-range seeking or HLS segments when transcoding is needed. It also records per-user playback state and watch sessions. Timeline markers, scrub sprites, and hover previews are generated as needed.

### Continue watching and Next up

The home page keeps two rows for you. **Continue watching** lists titles you started and did not finish, each with where you stopped ("S1E3 · Resume at 34:12") and a progress bar; a title drops off once you are past 95% of it. **Next up** lists, for every show or series you have been watching, the episode after the furthest one you finished. When an episode ends, an "Up next" card counts down and starts the following episode; Cancel stays on the finished one. The player's Next button and `Shift+N` do the same without waiting.

### Audio and subtitles

The player's settings menu (the gear) lists every audio track in the file and every subtitle track: text subtitles stored in the file, and sidecar files next to it (`Movie.en.srt`, `Movie.eng.forced.srt`, `Movie.es.vtt`, or the same names inside a `Subs` or `Subtitles` folder). Subtitles are shown as WebVTT; `.srt`, `.vtt`, `.ass`, and `.ssa` are supported, and so are embedded SubRip, ASS, and MP4 text tracks. Picture-based subtitles (PGS, VobSub) cannot be shown in a browser and are not offered. `C` toggles subtitles.

**Admins** see every title's tracks in its settings under **Audio & subtitles**: the audio tracks, the subtitles in the file and beside it (a subtitle file can be deleted there), and a search on OpenSubtitles.com. Connect an account under **Admin → Providers → Subtitles** (an API key to search; the account's username and password to download). A downloaded subtitle is saved next to the video, for example `Movie.en.srt`, so other players see it and the organizer moves it with the video. **Admin → Videos** shows each title's subtitle and audio track counts and filters by them. Tracks are read when a file is scanned; the **Read audio & subtitle tracks** job (run after every scan, or by hand under Jobs) fills in older files and notices subtitle files added since.

Choosing a different audio track reloads the video at the same position. When the video is H.264 and the track is AAC or MP3, the server hands the browser a stream-copy of the file with that track (cheap); any other track is served through the transcoder, and the Transcode button then stays on. Set your defaults under **Settings → Audio and subtitles**: an audio language, and subtitles off, always on, or only when the audio is in another language. A forced track in your language still shows with audio in that language.

### Reels

**Reels** (in the sidebar) plays the selected library's portrait videos, one at a time in a vertical player like YouTube Shorts or Instagram Reels. A video is a reel when it displays taller than it is wide; phone videos recorded as a rotated landscape frame count too. Nothing needs tagging: scans read each file's frame size and rotation, and reels appear after the next scan (files scanned before reels existed are re-read once, in the background, when the server starts). Each library has its own reels; switching library switches them. Reels also stay in the library like any other title, and a portrait title's page shows a tall player with the details beside it. **Admins** can override the detection under the title's **Settings → Reel**: Automatic, Always a reel, or Never a reel.

Scroll, swipe, or use `↑`/`↓` (or `J`/`K`) to move between reels; one wheel notch moves one reel. Tap the video or press `Space` to pause, `M` to mute. Reels loop, and watching most of one counts it as watched. The buttons beside the video favorite it, pause, mute, and open its details. **Shuffle** gives a new random order each visit; **Newest** lists the latest additions first.

### Playlists and smart playlists

A playlist is either **manual** (you pick and order the titles) or **smart**. A smart playlist is a saved filter: choose whether titles must match all or any of your conditions, and it fills itself, staying current as the library changes. Conditions include watched or not, favorite, Watch later, your rating or the average (stars), tag, category, studio, performer, series, words in the title or cast, movie or episode, release year, runtime, and when it was added (the last N days, a year, or a date range). Sort it, and optionally cap it ("top 25 rated"). Examples: *Unwatched Marvel* (text "Marvel" + not watched), *Saved from 2024* (added year 2024), *Weekend picks* (rating at least 4 stars + added in the last 30 days). Smart playlists show a Smart badge; edit the rules or name from the playlist page. You cannot add, remove, or reorder titles in one.

Favorites, ratings, watch later, playback history, and playlists belong to the signed-in user. Shared title metadata, posters, and associations are managed through Admin permissions.

## Admin workflows

| Screen | Use it for |
| --- | --- |
| **Dashboard / Analytics / Health** | Overview, usage and storage charts, service health |
| **Libraries / Users** | Library roots, modes, order, and access grants |
| **Storage / Scan rules / Jobs** | Mounts, scans, inclusion rules, background work |
| **Automation** | Scheduled scans (cron) and file watching, so new files appear on their own |
| **Housekeeping** | Find and remove leftovers: unused thumbnails and artwork, stale caches, unfinished files, old history |
| **Videos / Review / Rename** | Inspect indexed files, confirm metadata, preview and apply names |
| **Metadata** | Providers (including [plugins](provider-plugins.md)), performers, studios, series, categories |
| **Organizer** | Per-library rules, dry-run preview, organization and cleanup across every disk the library is on |
| **Duplicates** | Full-content duplicate scan and review |
| **Settings / Notifications** | App settings, webhook and notification configuration |

Preview an organizer or rename action before applying it. These actions can change files on the mounted storage. Duplicate scanning reads full files and can take much longer than an incremental library scan.

### Scheduled scans and file watching

**Admin → Automation** has two parts. *Scheduled jobs* run a scan (one library or every library) or a duplicate scan on a cron schedule: pick a preset such as "Every night at 03:00", or write a five-field expression (`minute hour day month weekday`; lists, ranges, and `*/15` steps work; `@hourly`, `@daily`, `@weekly`, and `@monthly` are accepted). Times use the server's time zone (set `TZ` for the container). A run missed while the server was down is not made up. *Watch for new files* follows a library's folders and scans that library about 45 seconds after the last video or subtitle file changes, so copying a season in one go causes one scan. Watching is off by default. It only sees changes made on this machine. On a network share, ReelStash also checks the folders every two minutes and scans when a video or subtitle file changed. A schedule is still the right choice when watching is off.

A scheduled scan that fails, and a schedule or watcher that cannot start, notify admins like any failed job: an in-app notification and every webhook subscribed to *Scan failed* or *Job failed* (**Admin → Notifications**). A scan never starts on a disk that is already being scanned or organized; the watcher tries again a minute later.

### Housekeeping

**Admin → Housekeeping** clears out what builds up over time. **Analyze** runs every task as a dry run and shows what each would remove, with examples under *Details*; nothing is deleted until you pick tasks and **Clean up**. Tasks come in three groups:

- *ReelStash data folders*: thumbnails, previews and artwork no record uses; streaming segments and remuxed copies of removed videos, or ones not played within the cache period (remuxed copies are full video copies, so this is usually the biggest saving); outdated converted subtitles; half-written `.part` files and empty folders; interrupted plugin installs.
- *Database*: records of thumbnails and artwork whose file is gone (made again on the next scan or play), unused artwork, provider links to deleted items, and job history, notifications, expired sign-ins and disk-capacity readings older than their retention period. *Titles without files* and *Settings of removed plugins* are opt-in. *Optimize database* runs `VACUUM ANALYZE`.
- *Library folders* (opt-in): NFO, subtitle and `-poster`/`-thumb` files whose video is no longer beside them, and `.DS_Store`, `Thumbs.db`, `desktop.ini` and `._` files. These delete files beside your videos; disks being scanned or organized are skipped.

Files changed in the last hour are never touched, and a data folder that looks unmounted (unreadable, or missing most of its files) is skipped rather than emptied. Set the retention periods at the bottom of the page. To run it regularly, add a *Housekeeping* schedule under **Automation**; scheduled runs use the default tasks and never touch library folders.

## Files app

Files lists configured storage locations and lets users browse and download within their granted library scope. Admins can also create directories, upload, move, rename, delete files, and edit NFO files. These actions affect the actual mounted filesystem. If a file changes outside ReelStash, run a scan to reconcile database records. Entries with a lock are the downloads in-progress folder or torrent data qBittorrent is using; Files won't change them ([Downloads](downloads.md)).

## Notifications and events

The UI subscribes to `/api/events` for live job and notification updates. Admins can configure webhooks; job completion or failure can produce notifications. If updates stop appearing, refresh the page and inspect the API and UI proxy logs as described in [Operations](operations.md).
