# Plugin bundles

A **plugin bundle** is a plugin ReelStash installs and runs for you. It is a folder (or a `.zip` of one) with a `plugin.json` and the script it starts. You add it under **Admin → Plugins**; ReelStash unpacks it, starts the script on a private port, connects it, restarts it if it crashes, and stops it when you switch it off.

A bundle speaks one of two HTTP contracts:

| `kind` | Contract | What it adds |
| --- | --- | --- |
| `provider` | [Provider plugins](provider-plugins.md) | A metadata source, used for matching, like TMDB |
| `auth` | [Auth plugins](auth-plugins.md) | A way to sign in, such as OpenID Connect |

Everything in those contracts also works without a bundle: you can run a provider as your own service and connect it by address (**Admin → Metadata → Providers**). A bundle just saves you running and wiring the service yourself.

## Installing

ReelStash ships with **no plugins installed**, and the Plugins page lists nothing until you add one (a plugin store may come later). Plugins live in their own repository, **`reelstash_plugins`** (next to this one), where each plugin is written, versioned and built; its `dist/<id>/<id>-<version>.zip` files are what you install. **Admin → Plugins** offers two ways:

- **From a file**: upload a `.zip`.
- **From an address**: ReelStash downloads a `.zip` from a URL.

Installing a bundle whose id is already installed **upgrades** it. Its settings, its data folder and its on/off switch are kept.

Installed bundles live under `REELSTASH_PLUGIN_DIR` (default `/app-data/plugins`, a volume in the Compose files), one folder per bundle: `app/` for the unpacked files, `data/` for whatever the plugin wants to keep.

> **A bundle is a program that runs on your server.** It runs as the same user as ReelStash and can read what that user can read: your media mounts, the plugin folder, the network. Only install bundles from people you trust, and treat Admin access accordingly. To turn the whole feature off, set `REELSTASH_PLUGIN_BUNDLES=false`; nothing can then be installed or started.

## Updating

Installing a bundle whose id is already installed **upgrades** it in place (settings, data folder and the on/off switch are kept), and ReelStash says so: "My plugin updated: 1.0.0 → 1.1.0". You can do that by hand with **Add plugin**, or let ReelStash find and install new versions:

- Give the plugin an **update source**: the address of its `index.json` (the `reelstash_plugins` build writes one at `dist/index.json`: every released version with its file and sha256). Set it per plugin under **Settings → Update source**, or have the plugin name it with `"updateUrl"` in its `plugin.json`. An address saved in the app wins.
- A plugin with a source is checked when the Plugins page opens: the list shows **Update 1.1.0 available** and an **Update** button. **Settings → Check for updates** checks on demand.
- **Update** downloads the newer version's file (relative to the index), checks its sha256 against the index, and installs it. A download that does not match is refused and nothing changes.

The checksum guards against a corrupted download, not a malicious source: an update source is trusted to the same degree as the plugin itself, since an update is code that runs on your server. Point it only at an address you control or trust.

## Making a bundle

```
my-plugin/
  plugin.json
  server.py
```

```json
{
  "id": "my-plugin",
  "name": "My plugin",
  "version": "1.0.0",
  "kind": "provider",
  "description": "One sentence shown on the Plugins page.",
  "run": { "command": ["python3", "server.py"] },
  "config": [
    { "key": "apiKey", "label": "API key", "env": "MY_API_KEY", "secret": true, "required": true }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `id` | 2-31 lowercase letters, digits or dashes. Must equal the `id` the service reports in its `/manifest`. A provider is registered as `x-<id>`. |
| `name`, `version`, `description`, `homepage` | Shown on the Plugins page. `version` is required. |
| `kind` | `provider` or `auth`. |
| `run.command` | The program and its arguments, run from the bundle's folder. The program is either a name found on `PATH` (`python3`, `node`) or a file inside the bundle starting with `./` (`./server`). Absolute paths and `..` are refused. |
| `updateUrl` | Optional. The address of the `index.json` that lists the plugin's published versions (see Updating). |
| `config` | Settings the admin fills in on the Plugins page. Each becomes an environment variable for the service: `env`, or the upper-cased `key` when omitted. `secret` masks the value, `required` keeps the plugin from starting until it is set, `default` is used while nothing is saved. |

Add it to the `reelstash_plugins` repo as `plugins/my-plugin/` and run `scripts/build.py my-plugin` there; that writes `dist/my-plugin/my-plugin-<version>.zip`, with `plugin.json` at the top level (a single wrapping folder is also accepted). The repo's README covers versioning: released versions are immutable, so a change means bumping `version` and adding a changelog entry. Working bundles to start from: `example-provider` (a metadata provider) and `oidc-auth` (a sign-in method).

Limits: 64 MB per `.zip`, 200 MB and 5,000 files unpacked. Links and special files are refused, as are paths that point outside the bundle.

The program must be available where ReelStash runs. The Docker image includes **Python 3**; for anything else, ship a single static binary (built for the server's CPU) and use `"command": ["./my-plugin"]`, or build your own image.

## What the service gets

ReelStash starts the command with:

| Environment variable | Meaning |
| --- | --- |
| `HOST` | Always `127.0.0.1`. Listen there only. |
| `PORT` | A free port ReelStash picked. |
| `REELSTASH_PLUGIN_TOKEN` | A random secret. Every request ReelStash makes carries `Authorization: Bearer <token>`; refuse requests without it (the port is only on the loopback interface, but other programs on the machine can reach it). |
| `REELSTASH_PLUGIN_ID`, `REELSTASH_PLUGIN_KIND` | The bundle's id and kind. |
| `REELSTASH_PLUGIN_DATA` | A folder that survives restarts and upgrades. Also `HOME`. |
| your `config` keys | The values saved on the Plugins page. |

It does **not** get ReelStash's own environment (no `DATABASE_URL`, no secrets), only `PATH`, `LANG`/`LC_ALL`, `TZ` and the CA-bundle variables. Whatever it prints to stdout or stderr appears under **Show log** on its card (the last 500 lines).

ReelStash considers the service ready when `GET /manifest` answers `200` with the token (within 30 seconds). It then reads the manifest, checks that its `id` matches the bundle's, and connects it.

## Lifecycle

- **Starting**: when ReelStash starts, or when you install, switch on or change the settings of a bundle.
- **Needs settings**: a `required` setting is empty, so nothing runs yet.
- **Crashes**: the service is restarted after 2, 4, 8, 16 and 30 seconds. After six crashes in a row it is marked **Not working** with the last error; press **Restart** once the problem is fixed. A run of a minute or more resets the count.
- **Switching off** stops the process. A provider stays listed (so titles it matched keep its name) but is not used for matching; a sign-in plugin disappears from the sign-in screen.
- **Uninstalling** deletes the files, the data folder and the settings. A provider's links on titles stay, and libraries stop listing it.
- **Restarting ReelStash** stops every bundle and starts the enabled ones again.

## Provider bundles

Besides the contract itself, a provider bundle gets the same **on/off switch** and **request log** as a provider connected by address (Admin → Metadata → Providers → *Request log* shows the last 100 requests ReelStash made: what was asked, the status, the time, and any error). Library selection, priority and testing work as for any provider. The generated token replaces the optional access token of an address-connected plugin, so a bundled provider has no token field.
