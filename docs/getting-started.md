# Getting started

Just want to run ReelStash on a server or home machine? Use the [one-command installer](install.md). The sections below are for developing ReelStash.

## Local development

Install Docker, Go 1.26.5, Node.js 22, and npm. Then run from the repository root:

```bash
npm ci --prefix ui
scripts/dev.sh
```

`scripts/dev.sh` starts a PostgreSQL 16 container named `reelstash-dev-postgres`, runs the Go API, and starts Vite. Open <http://localhost:3000>. The API health endpoint is <http://localhost:8902/healthz>. The script defaults to local emergency authentication with password `admin`; set `EMERGENCY_LOCAL_PASSWORD` before running it if others can reach your machine.

Vite proxies `/api`, `/auth`, and `/healthz` to the Go API. On this one-origin development setup, `/admin` and `/files` are also available from the same Vite server. Stop the application with Ctrl+C. To stop its database container too:

```bash
docker stop reelstash-dev-postgres
```

## Local Docker Compose

The root `compose.yml` builds the backend and UI and starts PostgreSQL. It is a simple local stack, distinct from any production stack.

1. Copy `.env.local.example` to `.env.local` (`scripts/dev-docker.sh` does this on first run) and set `MEDIA_ROOT` to an existing host directory. It is mounted at `/mnt` in the API container.
2. Set a unique `POSTGRES_PASSWORD` in `.env.local`. It defaults to `reelstash` for this local example.
3. Nothing to configure for sign-in: the first time you open the app it asks you to create the admin account. To create it from the environment instead, set `REELSTASH_ADMIN_USERNAME` and `REELSTASH_ADMIN_PASSWORD`. To use an identity provider, install an [auth plugin](auth-plugins.md) later.
4. Start the stack:

```bash
scripts/dev-docker.sh up        # also: down | logs | build
```

Open <http://localhost:3091>. The API is published on <http://localhost:8902>. The database and generated assets are stored under `./data/`; the media files stay in your host mount.

The root Compose example uses ReelStash's own accounts: the first visitor creates the admin. Do that before exposing the port to anyone else, since whoever gets there first becomes the admin.

## First library and scan

**The easy way: the setup wizard.** On a fresh install, opening **Admin** shows it automatically (it is always available under **Admin → System → Setup wizard**). It has four steps: choose *Entertainment* (movies and TV) or *Private videos*, enter the media folder and let ReelStash check it (it counts the videos it can see and tells you if the path is wrong), optionally paste a TMDB key and test it (for private videos it points you to the provider plugins), then it creates the disk and library and runs the first scan with live progress. You can skip the key and add it later.

**The manual way.**

1. Sign in and open **Admin**. With ReelStash's own accounts the admin you created has the Admin role. With a sign-in plugin, list your identity provider subject in the plugin's admin subjects setting so your account receives it. Local emergency login grants administrative access.
2. Create a library in **Admin → Libraries**. Select its mode and configure access for users as needed.
3. Open **Admin → Storage** and add a storage location. Enter the **container-visible absolute path**, such as `/mnt/movies`, not the host path to the left of the Compose volume colon. Supported types are `LOCAL`, `EXTERNAL`, `NAS`, `NETWORK_SHARE`, and `ARCHIVE`.
4. Associate the storage root with a library in the Libraries screen. Add scan rules if you need path exclusions or selective scanning.
5. Start a scan from Storage and watch progress in **Admin → Jobs**. Indexed media appears in the media app. Review unmatched metadata in **Admin → Review** or configure providers in **Admin → Metadata → Providers**.

The scanner recognizes `.mp4`, `.mkv`, `.avi`, `.mov`, `.m4v`, `.wmv`, `.ts`, and `.webm` video files. It can read adjacent NFO files. Generated thumbnails, posters, sprites, and previews need writable asset directories.

## Next steps

- [User guide](user-guide.md) for daily workflows.
- [Configuration](configuration.md) for authentication, storage, and worker settings.
- [Operations](operations.md) before deploying to a shared host.
