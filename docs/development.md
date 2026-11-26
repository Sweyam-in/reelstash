# Development

The iOS and Android apps are maintained in the separate [reelstash-app Flutter repository](https://github.com/Sweyam-in/reelstash-app). The `media/` directory in this repository is the React web media library and `admin/` the admin app.

## Repository map

```text
backend/internal/server/            Process wiring and lifecycle for every role
backend/cmd/media-server/           Media API server (no jobs)
backend/cmd/admin-server/           Admin API server and background jobs
backend/cmd/server/                 Both in one process (local, SQLite)
backend/internal/httpapi/router.go  Route registration and middleware
backend/internal/config/config.go   Environment configuration
backend/internal/database/          Pool and embedded SQL migrations
backend/internal/jobs/              Persisted background jobs
backend/internal/scanner/           Discovery and media processing
backend/internal/namer/             Metadata matching
media/src/App.jsx                      browser media library routes
admin/src/AdminApp.jsx                 reelstash-admin routes
admin/src/FilesApp.jsx                 Files workspace routes (served with the admin)
media/src/api.js, admin/src/api.js     Each app's own API client
media/vite.config.js, admin/vite.config.js   Development proxy and entries
media/nginx/, admin/nginx/            Production static serving and proxy
scripts/test.sh                        Combined test command
```

## Run and build

Install Go 1.27.4, Node.js 24, npm, and Docker. The backend Dockerfile installs ffmpeg and ffprobe; install them on the host for direct Go development.

```bash
npm ci --prefix media
npm ci --prefix admin
scripts/dev.sh
```

The media app is on port `3000`, the admin app on `3001`, the media API on `8902`, the admin API on `8903`, and the development database on `5432`. For standalone commands:

```bash
cd backend && go run ./cmd/server          # both APIs in one process on :8902
cd media && npm run dev    # :3000, talks to :8902
cd admin && REELSTASH_ADMIN_API=http://localhost:8902 npm run dev   # :3001, against the one-process server
cd media && npm run build
cd admin && npm run build
```

The standalone backend command needs a reachable PostgreSQL database through `DATABASE_URL` and suitable authentication settings. The launcher supplies local defaults for both.

## Tests

```bash
scripts/test.sh
scripts/test.sh backend
scripts/test.sh ui
```

The Go command runs `go vet ./...` and `go test ./... -count=1`. UI tests include Node unit tests and Vitest component tests. The details of scratch database setup, isolated schemas, and test conventions are in [TESTING.md](../TESTING.md). Database integration tests skip when `REELSTASH_TEST_DATABASE_URL` is unset and the development Postgres container is unavailable; check that they actually ran when changing persistence or API behavior.

## Make a backend change

1. Add or adjust a handler under `internal/httpapi/` and register its route in `router.go` with the right authentication, Admin, and library checks.
2. Add PostgreSQL schema changes to `internal/database/migrations/postgres.sql`. Keep changes safe to rerun: the consolidated script is embedded and replayed at startup. Preserve existing-install upgrade behavior as well as fresh-install behavior.
3. If the change runs asynchronously, register a handler in `internal/server` (`registerJobs`) or `scanner.RegisterJobs` and persist work through `jobs.Runner`.
4. Add a meaningful test for behavior that can regress: router authorization, database state, job lifecycle, or a parsing edge case. See the existing test harness.
5. Update this documentation and the frontend API client if the endpoint or user workflow changed.

## Make a UI change

reelstash-media and reelstash-admin are independent Vite projects with their own dependencies, tests, build and image; they share no source and meet only over the HTTP API and cross-origin links. each app's `src/api.js` is the request layer; use it to keep endpoint usage discoverable. The UI sends library context through headers and query parameters, while the backend makes the access decision. Run `npm test` and `npm run build` from `media/` or `admin/` for behavior and bundle checks.

New UI API calls must correspond to a route on that app's own server; the backend contract test checks the media app against the media server and the admin app against the admin server. Admin actions belong in the admin app. When changing authentication or cross-app links, test both the one-origin Vite setup and the paired-origin production configuration.

## Documentation maintenance

Keep `README.md` short and link to the relevant guide here. Update `configuration.md` when adding environment variables, `api.md` when changing route groups or conventions, and `architecture.md` when changing service boundaries, jobs, or persistence. Use exact code and Compose files as the source for defaults.
