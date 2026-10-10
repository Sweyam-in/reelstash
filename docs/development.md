# Development

## Repository map

```text
backend/cmd/server/main.go          Process wiring and lifecycle
backend/internal/httpapi/router.go  Route registration and middleware
backend/internal/config/config.go   Environment configuration
backend/internal/database/          Pool and embedded SQL migrations
backend/internal/jobs/              Persisted background jobs
backend/internal/scanner/           Discovery and media processing
backend/internal/namer/             Metadata matching
app/src/App.jsx                        reelstash-app routes (media library)
admin/src/AdminApp.jsx                 reelstash-admin routes
admin/src/FilesApp.jsx                 Files workspace routes (served with the admin)
app/src/api.js, admin/src/api.js       Each app's own API client
app/vite.config.js, admin/vite.config.js   Development proxy and entries
app/nginx/, admin/nginx/              Production static serving and proxy
scripts/test.sh                        Combined test command
```

## Run and build

Install Go 1.26.5, Node.js 22, npm, and Docker. The backend Dockerfile installs ffmpeg and ffprobe; install them on the host for direct Go development.

```bash
npm ci --prefix app
npm ci --prefix admin
scripts/dev.sh
```

The Vite server is on port `3000`, the Go API on `8902`, and the development database on `5432`. For standalone commands:

```bash
cd backend && go run ./cmd/server
cd app && npm run dev      # :3000
cd admin && npm run dev    # :3001
cd app && npm run build
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
2. Keep SQL migrations in `internal/database/migrations/`, numbered after the latest migration. They are embedded and applied in filename order at startup. Make reruns safe, following existing migrations.
3. If the change runs asynchronously, register a handler in `main.go` or `scanner.RegisterJobs` and persist work through `jobs.Runner`.
4. Add a meaningful test for behavior that can regress: router authorization, database state, job lifecycle, or a parsing edge case. See the existing test harness.
5. Update this documentation and the frontend API client if the endpoint or user workflow changed.

## Make a UI change

reelstash-app and reelstash-admin are independent Vite projects with their own dependencies, tests, build and image; they share no source and meet only over the HTTP API and cross-origin links. each app's `src/api.js` is the request layer; use it to keep endpoint usage discoverable. The UI sends library context through headers and query parameters, while the backend makes the access decision. Run `npm test` and `npm run build` from `app/` or `admin/` for behavior and bundle checks.

New UI API calls must correspond to a registered backend route; the backend contract test checks that relationship. When changing authentication or cross-app links, test both the one-origin Vite setup and the paired-origin production configuration.

## Documentation maintenance

Keep `README.md` short and link to the relevant guide here. Update `configuration.md` when adding environment variables, `api.md` when changing route groups or conventions, and `architecture.md` when changing service boundaries, jobs, or persistence. Use exact code and Compose files as the source for defaults.
