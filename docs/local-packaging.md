# Local packages

ReelStash can use PostgreSQL as before, or a local SQLite file. The SQLite
driver is the native SQLite implementation through `mattn/go-sqlite3`; builds
need a C compiler. Set `DATABASE_URL=sqlite:///absolute/path/reelstash.db`.
Keep the database on a local filesystem. Back up the SQLite database using
SQLite's online backup mechanism or `VACUUM INTO` while the server runs.

## macOS

Run `scripts/package-macos.sh` on macOS with Go, Node.js, npm, a C compiler,
and `hdiutil` installed. The output is `dist/ReelStash-macos.dmg`. Opening the
app starts the server on `127.0.0.1:8902` and opens the web UI. Data is stored
under `~/Library/Application Support/ReelStash`. The build downloads a pinned
[Jellyfin FFmpeg](https://github.com/jellyfin/jellyfin-ffmpeg/releases/tag/v8.1.3-2)
archive for the host architecture, verifies its SHA-256 digest, and bundles
`ffmpeg`, `ffprobe`, and their license. Users do not need to install FFmpeg.
For public distribution, sign and notarize the app and publish the corresponding
FFmpeg source and license information alongside the DMG. This Jellyfin build
enables GPL components; check its complete redistribution obligations before
publishing a release.

## Linux Snap

Run `snapcraft` at the repository root to build `snap/snapcraft.yaml`. The snap
bundles the server, both web UIs, and FFmpeg, and stores the database under
`SNAP_USER_COMMON`. Install the resulting `.snap` with `snap install --dangerous
<file.snap>` for local testing. Connect `reelstash:removable-media` when media
is on an external drive. Snapcraft is not available in this macOS development
environment, so the snap still needs a Linux build and installation test.

PostgreSQL remains the default for existing server and Docker deployments.
