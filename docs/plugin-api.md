# Plugin API reference

Everything a plugin author needs on one page. Each section links to the guide that goes into depth.

A ReelStash plugin is **a small HTTP service**. ReelStash calls it; it never calls ReelStash. It can be written in any language. There are two kinds:

| Kind | Adds | Contract |
| --- | --- | --- |
| `provider` | A metadata source for matching titles, people, studios and series | [Provider plugins](provider-plugins.md) |
| `auth` | A way to sign in (an identity provider, or your own user store) | [Auth plugins](auth-plugins.md) |

There are two ways to run one:

- **Bundle** (recommended). A `.zip` with a `plugin.json`; ReelStash installs it, starts it on a private port with a private token, restarts it if it crashes and stops it when switched off. See [Plugin bundles](plugin-bundles.md).
- **Your own service**, connected by address (providers only): **Admin → Metadata → Providers → Add a provider plugin**. Useful for a sidecar container.

> **Trust.** A plugin is code that runs on your server with the permissions of the ReelStash process, and an auth plugin decides who is signed in. Install only plugins you trust. Switch the whole feature off with `REELSTASH_PLUGIN_BUNDLES=false`.

## A plugin in five minutes

```
hello-provider/
  plugin.json
  server.py
```

`plugin.json`:

```json
{
  "id": "hello-provider",
  "name": "Hello provider",
  "version": "1.0.0",
  "kind": "provider",
  "description": "Matches one film.",
  "run": { "command": ["python3", "server.py"] }
}
```

`server.py` (standard library only; the Docker image includes Python 3):

```python
import json, os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TOKEN = os.environ["REELSTASH_PLUGIN_TOKEN"]
FILM = {"id": "1", "title": "Hello", "date": "2001-01-01"}

class H(BaseHTTPRequestHandler):
    def send(self, status, body):
        raw = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def authorized(self):
        return self.headers.get("Authorization") == "Bearer " + TOKEN

    def do_GET(self):
        if not self.authorized():
            return self.send(401, {})
        if self.path == "/manifest":
            return self.send(200, {"id": "hello-provider", "name": "Hello provider",
                                   "capabilities": {"media": True}})
        if self.path == "/media/1":
            return self.send(200, FILM)
        self.send(404, {})

    def do_POST(self):
        if not self.authorized():
            return self.send(401, {})
        self.rfile.read(int(self.headers.get("Content-Length") or 0))
        if self.path == "/media/search":
            return self.send(200, {"candidates": [{"score": 90, "autoMatch": False, "result": FILM}]})
        self.send(501, {})

ThreadingHTTPServer((os.environ["HOST"], int(os.environ["PORT"])), H).serve_forever()
```

Zip the two files with `plugin.json` at the top level, then **Admin → Plugins → Install a bundle → From a file**. Tick the provider for a library under **Admin → Libraries** and run **Test connection** on its card. A fuller example is `plugins/example-provider` in the `reelstash_plugins` repository.

You can also try a service by hand before packaging it:

```bash
HOST=127.0.0.1 PORT=9100 REELSTASH_PLUGIN_TOKEN=t python3 server.py &
curl -H 'Authorization: Bearer t' localhost:9100/manifest
```

## Bundle format

Full detail: [Plugin bundles](plugin-bundles.md#making-a-bundle).

| `plugin.json` field | Required | Meaning |
| --- | --- | --- |
| `id` | yes | 2-31 lowercase letters, digits or dashes; must equal the `id` in the service's `/manifest`. Providers are registered as `x-<id>` and matches store that id, so never change it. |
| `version` | yes | Semantic version. Installing the same `id` again upgrades in place and keeps settings, data and the on/off switch. |
| `kind` | yes | `provider` or `auth`. |
| `run.command` | yes | Program and arguments, run from the bundle folder. A name on `PATH` (`python3`, `node`) or `./file` inside the bundle. No absolute paths, no `..`. |
| `name`, `description`, `homepage` | no | Shown on the Plugins page. |
| `updateUrl` | no | Address of an `index.json` listing published versions; enables **Update available**. |
| `config[]` | no | Settings the admin fills in, passed to the service as environment variables: `key`, `label`, `env` (default: upper-cased `key`), `secret`, `required`, `default`, `placeholder`. `PATH`, `HOME`, `PORT`, `HOST`, `PWD`, `SHELL`, `USER`, `REELSTASH_*`, `LD_*`, `DYLD_*` are reserved. |

Limits: 64 MB per `.zip`; 200 MB and 5,000 files unpacked; links and special files are refused.

## What the service receives

| Environment | Meaning |
| --- | --- |
| `HOST`, `PORT` | Listen on exactly this, always `127.0.0.1` and a free port. |
| `REELSTASH_PLUGIN_TOKEN` | Every request carries `Authorization: Bearer <token>`. Reject anything else with `401`. |
| `REELSTASH_PLUGIN_ID`, `REELSTASH_PLUGIN_KIND` | The bundle's id and kind. |
| `REELSTASH_PLUGIN_DATA` | A folder that survives restarts and upgrades; also `HOME`. Keep state here. |
| your `config` keys | The saved settings. |

ReelStash's own environment (database URL, secrets) is **not** passed on; only `PATH`, `LANG`/`LC_ALL`, `TZ` and CA-bundle variables are. Output on stdout and stderr appears under **Show log** on the plugin's card (last 500 lines). Never log the token.

## Lifecycle

- Ready when `GET /manifest` answers `200` within 30 seconds; ReelStash then checks the `id`.
- Started at server start and on install, switch-on or settings change. A `required` setting that is empty means it does not start (**Needs settings**).
- Crash restarts after 2, 4, 8, 16 and 30 seconds; after six crashes in a row it is **Not working** until restarted by hand. A run of a minute or more resets the count.
- Switching off stops it; uninstalling deletes files, data folder and settings.

## HTTP conventions

- JSON in and out. Request bodies to auth plugins are limited to 1 MB; ReelStash reads at most 4 MB from a provider and gives each request 25 seconds.
- `401`/`403` for a missing or wrong token. `404` for an unknown id. `501` for a capability you do not implement. Any other status `>= 300` is shown to the admin as an error.

## Provider endpoints

Declare what you implement in `/manifest` → `capabilities`; at least one of `media`, `performer`, `studio` must be true. Field-by-field detail is in [Provider plugins](provider-plugins.md).

| Capability | Endpoints |
| --- | --- |
| (always) | `GET /manifest` → `{id, name, capabilities, imageHosts, notice}` |
| `media` | `POST /media/search` `{fileName, text, searchType}` → `{candidates: [{score, autoMatch, studioHit, dateHit, result}]}`; `GET /media/{id}` → result |
| `performer` | `GET /performers/search?q=` → `{results}`; `GET /performers/{id}` |
| `studio` | `GET /studios/search?q=` → `{results}`; `GET /studios/{id}` |
| `series` (with `media`) | `GET /series/search?q=` → `{results}`; `GET /series/{id}` |

Artwork URLs are only downloaded from hosts listed in `imageHosts`.

## Auth endpoints

Detail: [Auth plugins](auth-plugins.md#the-contract).

| Endpoint | When | Request → response |
| --- | --- | --- |
| `GET /manifest` | always | `{id, name, login: {type, label}, subjectPrefix, callbackPath, logoutUrl}` |
| `POST /auth/start` | `login.type: "redirect"` | `{callbackUrl, redirectTo}` → `{url}` |
| `POST /auth/finish` | `login.type: "redirect"` | `{callbackUrl, query}` → `{identity, redirectTo}` or `{error}` |
| `POST /auth/login` | `login.type: "password"` | `{username, password}` → `{identity}` or `{error}` |

`identity` is `{subject, email, displayName, avatarUrl, role}`, where only `subject` is required and `role` is `ADMIN` or `USER`. `error` is one of `cancelled`, `not_authorized`, `account_disabled`, `login_failed`. ReelStash owns the user record, role changes, library access and the session cookie.

## Admin API for managing plugins

Admin-only routes under `/api/admin` (session cookie or a device token); see [HTTP API](api.md) for conventions.

| Route | Purpose |
| --- | --- |
| `GET /plugins` | Installed bundles with status, settings (secrets masked) and available updates |
| `POST /plugins/install` | Install or upgrade from an uploaded `.zip` (multipart) |
| `POST /plugins/install-url` | Install or upgrade from a URL |
| `PUT /plugins/{id}/enabled` | Switch on or off |
| `PUT /plugins/{id}/config` | Save settings; restarts the plugin |
| `POST /plugins/{id}/restart`, `GET /plugins/{id}/log` | Restart; read the log |
| `PUT /plugins/{id}/update-source`, `POST /plugins/{id}/check-update`, `POST /plugins/{id}/update` | Set the update source, check for a newer version, install it |
| `DELETE /plugins/{id}` | Uninstall |

The exact request shapes are in `backend/internal/httpapi/plugins.go`, which is authoritative.

## Packaging and releasing

The `reelstash_plugins` repository holds the maintained plugins and the tooling:

```bash
scripts/build.py my-plugin          # writes dist/my-plugin/my-plugin-<version>.zip and updates dist/index.json
scripts/bump.py my-plugin patch     # released versions are immutable; bump to change them
```

Host `dist/` anywhere reachable and point `updateUrl` at `dist/index.json` to let ReelStash offer updates (it verifies each download's sha256 against the index).

## Compatibility

The contracts are versioned by ReelStash releases, not negotiated at run time. Additions (new optional fields, new capabilities) are backwards compatible: **ignore fields you do not recognise**, and treat any field you do not need as optional. Anything that breaks existing plugins is listed in the release notes.
