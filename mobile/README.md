# ReelStash apps

Native media apps for ReelStash: browse and play from any ReelStash server, self-hosted anywhere. They are media apps only; all admin work (metadata, libraries, users) stays in the web admin console.

| Target | Status |
| --- | --- |
| Android phones and tablets | MVP 1 (`androidApp`) |
| iPhone and iPad | Dev build (`iosApp`): SwiftUI on the shared Kotlin client. iPad uses a sidebar; iPhone uses the bottom bar. |
| Android TV, Apple TV | Later |

## Layout

```text
mobile/
  shared/       Kotlin Multiplatform: API client (Ktor), models, server address
                rules, playback planner, watch-progress counting. No Android types.
                iosMain is the Swift bridge (callbacks, because suspend functions
                are not exported). It is compiled only with -Preelstash.ios=true.
  androidApp/   Jetpack Compose app: screens, Media3 player, Coil images.
  iosApp/       SwiftUI app for iPhone and iPad. AVPlayer plays what the shared
                playback planner chooses. Open ReelStash.xcodeproj.
  scripts/      dev-server.sh: a throwaway server with sample videos.
```

The Xcode project passes `-Preelstash.ios=true` itself, so `gradle.properties` can stay `reelstash.ios=false` and an Android-only machine never needs Xcode.

## What MVP 1 does

- **Any server**: enter its address (`nas.local:8000`, `https://media.example.com`). Plain HTTP on the home network and HTTPS with a user-installed CA both work. Several servers can be saved; switch in Settings.
- **Sign-in**: username and password, or *pairing*. With pairing, the app shows a code, and someone signed in on the web enters it at `/link` or under Settings → Devices. Pairing works with every sign-in method, including plugins. The app holds a revocable device token, listed under Settings → Devices on the web.
- **Browse**: Home (Continue watching, Next up, recommendations), Browse (movies or videos, shows or series, people, reels), Search, and My stuff (favorites, watch later, playlists). Everything follows the selected library, with posters for entertainment libraries and thumbnails for private ones.
- **Detail**: resume, favorite, watch later, watched, cast and crew, related titles, and seasons and episodes for shows.
- **Player** (Media3):
  - The app decides **direct play** from the device's own decoders: `.mkv`, HEVC, and AC-3 where the device has the decoder. It streams the original file with `?raw=1`. Otherwise it falls back to the server's **HLS transcode**, and it also falls back if a direct-played file fails to decode.
  - Sidecar and embedded subtitles. The audio language and subtitle mode come from the account's preferences.
  - The resume point is saved every 15s, plus watch sessions, matching the web player.
  - "Up next" countdown between episodes.
  - Landscape for wide videos, portrait for reels, and picture-in-picture.
- **PIN lock**: if the account has one, the app asks for it, and the server enforces it.

Not in MVP 1: admin features, offline downloads, Chromecast, a vertical-swipe reels player, playlist editing, and TV. Sign-in tokens live in the app's private storage, which backups exclude; encrypting them with the Android Keystore is a follow-up.

## Build and run

You need JDK 17 or newer and the Android SDK (platform 37). Android Studio provides both.

```bash
cd mobile
./gradlew :shared:allTests            # shared unit tests
./gradlew :androidApp:installDebug    # build and install on a running emulator/device
```

### iPhone and iPad

Open `iosApp/ReelStash.xcodeproj` on a Mac with Xcode. The first build compiles the shared Kotlin framework, then the SwiftUI app.

The simulator signs itself and needs no Apple ID. To run on your own iPhone or iPad, select it as the run destination and set Signing to your Personal Team (Xcode → Signing & Capabilities). That is a free development install; it is not an App Store build. iPad is the primary layout: a sidebar for Home, Browse, Reels, Library, and Settings, with wide poster grids. iPhone keeps the same screens behind a bottom bar.

Point the app at the dev server below. From the simulator that is `localhost:8912` (or `127.0.0.1:8912`). From a device on the same Wi-Fi, use your Mac's address, such as `192.168.1.20:8912`. Plain HTTP is allowed because this is a personal client for a server you run yourself.

### A server to develop against

`scripts/dev-server.sh` runs the API from `../backend` on port 8912. It uses its own database (`reelstash_mobile` in the dev Postgres container that `.scripts/dev.sh` starts) and sample videos it generates with ffmpeg: a movie with a subtitle, a two-episode show, an AVI that must transcode, and a portrait reel.

```bash
scripts/dev-server.sh          # leave running
scripts/dev-server.sh seed     # once, in another shell: libraries, scan, the test show
```

In the app, connect to `http://10.0.2.2:8912` from the emulator. Sign in as `mobile-admin` / `mobile-dev-password`, the script's test-only defaults (`MOBILE_DEV_USER` and `MOBILE_DEV_PASSWORD` override them). To test pairing without the web UI, approve the code with the API:

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8912/api/devices/pair -d '{"code":"ABCD-EFGH"}'
```

## Server requirements

The app needs a server with the app API: `GET /auth/server-info` must return `apiVersion` 1 or later. Older servers get a clear "needs an update" message. See [docs/api.md](../docs/api.md#app-sign-in-device-sessions).

For pairing links to point to the public site, set `REELSTASH_MEDIA_URLS` on the server. Otherwise the link uses the address the app connected to.
