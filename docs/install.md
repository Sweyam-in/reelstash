# Quick install

One command sets up a complete ReelStash stack on a Linux or macOS machine or server: PostgreSQL, the API, the media app, the admin app and, if you want it, qBittorrent for the [Downloads workspace](downloads.md).

You need **Docker with the Compose plugin** (v2) and nothing else. Run it as a user that can use Docker.

```bash
wget -qO- https://raw.githubusercontent.com/Sweyam-in/reelstash/main/scripts/install.sh | sh
```

or with curl:

```bash
curl -fsSL https://raw.githubusercontent.com/Sweyam-in/reelstash/main/scripts/install.sh | sh
```

It asks three things (every one has a default; press Enter to accept):

1. **Media folder**: where your videos live on the host. It is mounted at `/mnt` inside the containers, so `/srv/media/movies` becomes `/mnt/movies` in ReelStash.
2. **Address**: the IP or hostname you will open ReelStash at (detected for you).
3. **qBittorrent**: whether to also run qBittorrent, wired to the same media folder.

Then it generates strong passwords, writes the stack into `./reelstash/`, pulls the images, starts everything and prints the addresses and the admin login:

```
  Media app   http://192.168.1.20:3091
  Admin       http://192.168.1.20:3092
  Sign in as   admin
  Password     <generated>
```

Open **Admin**, run the setup wizard and point it at a folder under `/mnt`. Next: the [user guide](user-guide.md).

## Unattended or customised

Options go after `sh -s --`:

```bash
wget -qO- https://raw.githubusercontent.com/Sweyam-in/reelstash/main/scripts/install.sh | sh -s -- \
  --media /srv/media --host media.example.lan --qbittorrent --yes
```

| Option | Meaning |
| --- | --- |
| `--dir PATH` | Install folder (default `./reelstash`) |
| `--media PATH` | Media folder on the host |
| `--host NAME\|IP` | Address you open ReelStash at |
| `--admin-user NAME` | First admin's username (default `admin`); the password is generated |
| `--qbittorrent` / `--no-qbittorrent` | Add qBittorrent or not |
| `--ui-port N`, `--admin-port N` | Ports (default `3091`, `3092`) |
| `--tag TAG`, `--registry PREFIX` | Which images to install |
| `--from-source` | Build the images from the Git repository instead of pulling them |
| `--yes` | Never prompt; use defaults for anything not given |
| `--print-compose` | Print the `compose.yml` and exit |

If you would rather read before you run (recommended for anything you pipe into a shell), download the script, read it, then run it:

```bash
wget https://raw.githubusercontent.com/Sweyam-in/reelstash/main/scripts/install.sh
less install.sh
sh install.sh
```

## What it creates

Everything is in the install folder:

| File | Purpose |
| --- | --- |
| `.env` | All settings and the generated passwords (mode `600`). Edit it to change ports, addresses or the qBittorrent login. |
| `compose.yml` | The stack. Rewritten on every run of the installer, so do not edit it. |
| `compose.gpu.yml` | Only if the host has `/dev/dri`: hardware transcoding. |

Data lives in Docker volumes (`postgres_data`, thumbnails, posters, installed plugins and so on), never in your media folder. Your media is only changed by ReelStash features you use (the organizer, Files, Downloads move rules).

## Managing it

Run these from the install folder:

```bash
docker compose ps                  # status
docker compose logs -f reelstash   # API log
docker compose down                # stop; data is kept
```

**Update**: run the installer again in the same folder. It keeps `.env`, pulls newer images and restarts. Pin a release with `--tag`.

**Back up**: the PostgreSQL database plus the asset volumes; see [Operations](operations.md#backups-and-restore). Your media is separate.

**Remove everything**, including data: `docker compose down -v`.

## qBittorrent

With qBittorrent enabled, the installer shares the media folder with it at the same `/mnt` path, seeds its web login from the generated password and connects ReelStash to it, so **Downloads** works immediately at `http://<host>:3092/downloads`. Its own web UI is bound to `127.0.0.1:8080` (set `QBIT_BIND_ADDR=0.0.0.0` in `.env` and run the installer again to reach it from the network). Forward TCP and UDP port `6881` on your router for good speeds. qBittorrent runs as the owner of the media folder (`MEDIA_UID`/`MEDIA_GID` in `.env`); create library folders as that user so it can move finished torrents into them. More in [Downloads](downloads.md).

## Security

- Traffic is plain HTTP. For access from outside your network, put a reverse proxy with HTTPS (Caddy, nginx, Cloudflare Tunnel) in front of the two ports and set `REELSTASH_MEDIA_URLS` and `REELSTASH_ADMIN_URLS` in `.env` to the public addresses, then run the installer again.
- The admin password is also stored in `.env`. After signing in and changing it in the app, delete the `REELSTASH_ADMIN_PASSWORD` line.
- The database and API are not published to the host; only the two web ports (and qBittorrent's peer port) are.

## Troubleshooting

| Problem | Fix |
| --- | --- |
| `could not pull the images` | The registry may be private or unreachable. Check `docker login`, or use `--from-source`. |
| Port already in use | Use `--ui-port` and `--admin-port`. |
| No videos found after the wizard | The path must be the one **inside** the container (`/mnt/...`), not the host path. |
| Downloads shows "permission denied" on move | The destination folder is not writable by `MEDIA_UID`. Create it as the media owner. |
| Sign-in fails on a different address than the one you gave | Use the address you gave the installer, or add the other address to the `REELSTASH_*_URLS` lists in `.env`. |
