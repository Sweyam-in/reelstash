# Provider plugins

TMDB is built in. Any other metadata source can be added without changing ReelStash: write a small HTTP service that speaks the JSON contract below, then connect it in **Admin → Metadata → Providers → Add a provider plugin**. A connected plugin is used for matching exactly like a built-in provider: it can be ticked per library, ordered in the priority list, tested, and used in the manual match dialogs.

A working example in about 100 lines of standard-library Python is in the `reelstash_plugins` repo: `plugins/example-provider/server.py`.

You can run the service yourself and connect it by address (below), or package it as a [plugin bundle](plugin-bundles.md) and let ReelStash run it: add a `plugin.json` next to the script and install the `.zip` under **Admin → Plugins**. `example-provider` is both.

## Managing a connected plugin

Each plugin card under **Admin → Metadata → Providers** has:

- an **on/off switch**. Off keeps the plugin connected (titles it matched still show its name) but takes it out of matching, search dialogs and library choices. The choice survives restarts and *Refresh*.
- a **Request log**: the last 100 requests ReelStash made to it (time, method and path, HTTP status, duration, error). Search text appears in the paths; access tokens never do. The log is in memory and is cleared when ReelStash restarts.
- *Test connection*, *Refresh* (re-reads the manifest) and *Remove*.

## How it fits together

- ReelStash calls your service; your service never calls ReelStash.
- Plugin providers get the id `x-<manifest id>`. Matches store that id, so keep your ids stable.
- Only the **manifest** is required. Implement the endpoints for the capabilities you declare; answer `501` (or simply don't declare the capability) for the rest.
- Every request has a 25 second timeout and responses are limited to 4 MB.
- If you set an access token when connecting, it is sent as `Authorization: Bearer <token>` on every request. Answer `401` or `403` for a wrong token.
- Status codes: `404` for an unknown id, `501` for an unsupported operation, anything else `>= 300` is shown to the admin as an error.

**Security.** Whoever can open Admin can make the ReelStash server call any `http://` or `https://` address, including private ones. That is what lets a plugin run as a sidecar container, and it means Admin access should be treated as trusted. Artwork is only downloaded from hosts listed in `imageHosts`.

## `GET /manifest`

```json
{
  "id": "mydb",
  "name": "My Database",
  "capabilities": { "media": true, "performer": true, "studio": false, "series": false },
  "imageHosts": ["img.mydb.example"],
  "notice": "Data courtesy of My Database."
}
```

`id` is 2-31 lowercase letters, digits or dashes. At least one of `media`, `performer`, `studio` must be true. `notice` is shown next to the provider's settings. **Refresh** re-reads the manifest; the id must not change.

## Titles (`media`)

`POST /media/search` with `{"fileName": "...", "text": "", "searchType": ""}`. `fileName` is the raw file name; parse it however suits you. `text`, when set, is what an admin typed into a manual search. `searchType` is empty, `all`, `scenes`, `movies` or `shows`. Answer:

```json
{ "candidates": [
  { "score": 87.5, "autoMatch": false, "studioHit": false, "dateHit": true,
    "result": { "id": "123", "title": "The Title", "date": "2001-05-04" } }
] }
```

`score` is 0-100 (comparable only between your own candidates). `autoMatch: true` means you are confident enough for the match to be applied without review; the library's matching settings still decide whether that happens. At most 25 candidates are used; a candidate without an `id` is ignored.

`GET /media/{id}` returns one result. A result can carry:

| Field | Meaning |
| --- | --- |
| `id`, `title` | Required |
| `description`, `date` (`YYYY-MM-DD`), `studio`, `tags` (array), `sourceUrl` | Optional |
| `poster`, `background` | Image URLs (host must be in `imageHosts`) |
| `performers` (names), `performerImages` (name → URL), `performerIds` (name → your performer id) | Cast |
| `kind` (`movie` or `episode`), `show`, `showId`, `season`, `episode` | TV: episodes of one `showId` land in one series |
| `characters` (name → role) | Roles played |
| `crew` | `[{ "id", "name", "job": "director" or "writer", "imageUrl" }]` |

## People and studios

- `GET /performers/search?q=name` → `{ "results": [ ... ] }`, `GET /performers/{id}` → one. Fields: `id`, `name`, `disambiguation`, `bio`, `aliases`, `image`, `gender`, `birthdate`, `birthplace`, `ethnicity`, `nationality`, `hairColor`, `eyeColor`, `height`, `measurements`.
- `GET /studios/search?q=name` → `{ "results": [ ... ] }`, `GET /studios/{id}` → one. Fields: `id`, `name`, `shortName`, `description`, `url`, `network`, `image`, `aliases`.

## Series (optional)

Declare `"series": true` (together with `media`) and implement `GET /series/search?q=` → `{ "results": [ ... ] }` and `GET /series/{id}`. A series has `id`, `title`, `description`, `date`, `studio`, `kind` (`show`, `movie`, `collection`), `poster`, `sourceUrl`, `partCount` and `parts`: `[{ "id", "number", "label", "title", "date", "poster", "performers" }]`. Each part's `id` must work with `GET /media/{id}`.

## Try it

```bash
python3 ../reelstash_plugins/plugins/example-provider/server.py   # http://localhost:9100
curl localhost:9100/manifest
```

Then add `http://localhost:9100` in the Providers page (use the container's network address when ReelStash runs in Docker, for example `http://plugin:9100` on the same Compose network) and tick it for a library.
