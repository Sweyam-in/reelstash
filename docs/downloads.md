# Downloads (qBittorrent)

**ReelStash Downloads** is a workspace beside Admin and Files (`/downloads`, admins only) for managing a qBittorrent from ReelStash, without signing in to qBittorrent separately. From it you can:

- watch torrents live (progress, speeds, ETA, ratio, the library each one sits in);
- add magnet links, URLs or `.torrent` files, with a save folder, category and tags;
- start, stop, force start, recheck, move, rename and remove torrents; set file priorities and per-torrent limits;
- manage categories and tags, switch the alternative speed limits, and change a chosen set of qBittorrent settings;
- protect an **in-progress folder**, which nothing in ReelStash ever touches;
- have **move rules** put finished torrents into your libraries while they keep seeding.

The ReelStash server talks to qBittorrent; the browser never does. qBittorrent's credentials stay on the server, and only a fixed set of its settings can be changed from ReelStash. Its "run a program" options and WebUI settings are not among them.

## Running qBittorrent with Compose

The Compose files carry an optional `downloads` profile (the [installer](install.md) turns it on for you if you say yes): a `qbittorrent` service (the linuxserver image) that mounts the **same** `${MEDIA_ROOT}` at `/mnt` as ReelStash, so a path one app reports is a path the other can open. A one-shot `qbittorrent-init` service writes the WebUI username and password into qBittorrent's config before it starts, so you never copy the temporary password from its logs.

```bash
# .env.local (or your production env file)
COMPOSE_PROFILES=downloads
REELSTASH_QBIT_URL=http://qbittorrent:8080
QBIT_USERNAME=admin
QBIT_PASSWORD=change-me-please     # 6+ characters
QBIT_BIND_ADDR=0.0.0.0             # 127.0.0.1 keeps qBittorrent's own web UI to this machine
MEDIA_UID=1000                     # owner of the media on the host; qBittorrent runs as it
MEDIA_GID=1000
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `REELSTASH_QBIT_URL` | empty | qBittorrent's address as the API container sees it. Empty: Downloads shows "Not set up" until an address is saved in the app. |
| `QBIT_USERNAME`, `QBIT_PASSWORD` | `admin`, empty | Seeded into qBittorrent's config and handed to ReelStash as `REELSTASH_QBIT_USERNAME`/`PASSWORD`. |
| `QBIT_BIND_ADDR`, `QBIT_WEBUI_PORT` | `0.0.0.0`, `8080` | Where qBittorrent's own web UI is published on the host. |
| `TORRENT_PORT` | `6881` | Peer port (TCP and UDP). Forward it on your router for good speeds. |
| `MEDIA_UID`, `MEDIA_GID` | `1000` | User qBittorrent runs as (`PUID`/`PGID`). |
| `QBIT_IMAGE_TAG` | `latest` | linuxserver/qbittorrent tag. |

qBittorrent's web UI is published on the host as well, for when you want it. `qbittorrent-init` turns off qBittorrent's Host-header check, which would otherwise reject the published port and tunnel hostnames; the password still protects it. Anything changed there happens outside ReelStash, which reads qBittorrent's settings again every few minutes and warns about changes that matter (see Warnings below). Its password is always required: the Compose network is not exempt from sign-in, because published ports can make outside requests look as if they came from inside it.

**File ownership.** The ReelStash container runs as root, so folders it creates (organizer targets, folders made in Files) are owned by root. qBittorrent, running as `MEDIA_UID`, can't move torrents into a folder it can't write to, and the move fails with a permissions message. Create library folders as the media owner, or give that user write access.

**VPN.** To route torrent traffic through a VPN container (such as gluetun), put qBittorrent on that container's network (`network_mode: service:gluetun`) and point `REELSTASH_QBIT_URL` at the VPN container (`http://gluetun:8080`).

Any other qBittorrent works too: enter its address under **Downloads → Settings**. When it sees the media under another path, set **Path translation** (qBittorrent `/downloads` ⇄ ReelStash `/mnt/disk1/downloads`).

## The in-progress folder

Set it under **Downloads → Settings → Folders**. ReelStash hands it to qBittorrent ("Keep incomplete torrents in"), so unfinished downloads live there and land in their final folder only when complete. Inside it:

- scans (whole disk and library) never index anything, whatever the scan rules say;
- the file watcher never watches it, so half-written files never trigger scans;
- the organizer, organizer clean-up and housekeeping never change anything;
- Files lists it with a lock and refuses rename, move, delete, upload, new folders and NFO edits.

It can sit inside a library folder (a library may span a whole disk): being locked is what keeps scans out. It can't be, or hold, a library's own folder, and no library folder can be placed inside it. Locking a folder that already held indexed files leaves those rows alone; they are neither deleted nor marked missing.

## Torrents in use

Every torrent's data counts as **in use** while it is in qBittorrent, whether it is downloading or seeding. It is scanned and played like any other file, but ReelStash will not break the torrent:

- the organizer holds those files where they are (the preview counts them as *held*);
- renaming from the namer or the Rename Pipeline is skipped or refused;
- Files refuses to rename, move or delete them (copying is fine);
- deleting such a title in Admin is refused.

Move or remove the torrent from Downloads instead. Removing a torrent (keeping its files) frees them. Deleting a torrent **with** its files from inside a library removes that title on the library's next scan, along with its watch history. The remove dialog says so.

## Move rules

Under **Downloads → Move rules**. When **Move finished torrents** is on, a torrent that finishes is checked against the rules from top to bottom, and the first rule whose conditions all hold moves it:

| Condition | Matches when |
| --- | --- |
| Category | equal, ignoring case |
| Tag | the torrent has the tag |
| Tracker contains | the current tracker URL contains the text |
| Name | with `*` or `?`, a whole-name wildcard; otherwise a substring (case never matters) |
| At least (MB) | the torrent's wanted size |
| Only torrents with a video file | one of its files has a video extension the scanner indexes |

The destination is a folder on a storage location, plus an optional subfolder template (`{name}`, `{category}`, `{tag}`). **qBittorrent does the move** (`setLocation`), so the torrent keeps seeding from its new place. A move within one disk is instant; across disks it is a copy, and the torrent shows *Moving* meanwhile.

After the move ReelStash adds a tag (`reelstash` by default), scans the library the torrent landed in (retrying while another scan or the organizer holds the disk) and, if the rule says so, removes the torrent from qBittorrent once moved or once it reaches a ratio or seeding time. Removing never deletes files.

What the rules look at, and what they leave alone:

- only torrents that finish **after** auto-move was switched on; for older ones use **Run move rules now** in the torrent's details;
- only torrents in qBittorrent's default save folder or their category's folder. A torrent added with a folder of its own was placed on purpose and is skipped (running the rules by hand still moves it);
- each torrent once. **History** lists every result. Forgetting a record lets the rules look again.

**Test rules** shows what each finished torrent would get, without moving anything. Failed moves (qBittorrent can't write to the folder, the torrent errored, or nothing happened within two minutes) raise an admin notification, and the `download.failed` webhook event. Successful moves send `download.moved`.

## Warnings

The torrent list shows setup problems with a fix where one exists:

- **qBittorrent's in-progress folder no longer matches** the protected one (changed in its own web UI, or a fresh qBittorrent): *Apply to qBittorrent*.
- **qBittorrent keeps unfinished downloads in a folder ReelStash does not protect**: *Protect it*.
- **No in-progress folder**, or **qBittorrent saves straight into a library**: set one in Settings.

## API

All under `/api/downloads`, admins only. qBittorrent errors come back as `502` with its message; `503` means it is not set up.

```text
GET    /api/downloads                       torrents, categories, tags, transfer totals, warnings
POST   /api/downloads/add                   multipart: urls, torrents (files), storageLocationId+path, category, tags, stopped, sequential, firstLast, skipChecking
POST   /api/downloads/torrents/action       {hashes, action: start|stop|forceStart|noForceStart|recheck|reannounce|topPrio|bottomPrio|increasePrio|decreasePrio|toggleSequential|toggleFirstLast}
POST   /api/downloads/torrents/delete       {hashes, deleteFiles}
POST   /api/downloads/torrents/category     {hashes, category}
POST   /api/downloads/torrents/tags         {hashes, add, remove}
POST   /api/downloads/torrents/move         {hashes, storageLocationId, path, scan}
GET    /api/downloads/torrents/{hash}       properties, files, trackers
GET    /api/downloads/torrents/{hash}/peers
POST   /api/downloads/torrents/{hash}/rename | /files {ids, priority} | /limits {download, upload} | /apply-rules
POST   /api/downloads/categories            {name, storageLocationId, path};  PUT|DELETE /categories/{name}
POST   /api/downloads/tags                  {name};  DELETE /tags/{name}
POST   /api/downloads/alt-speed
GET|PUT /api/downloads/preferences          the allowed qBittorrent settings only
GET|PUT /api/downloads/settings             connection, path translation, autoMove, moveTag;  POST /settings/test
PUT    /api/downloads/folders               {incomplete | clearIncomplete, savePath};  POST /folders/fix {fix}
GET|POST /api/downloads/rules;  PUT|DELETE /rules/{id};  PUT /rules/order {ids};  GET /rules/test
GET    /api/downloads/history;  DELETE /history/{hash}
```
