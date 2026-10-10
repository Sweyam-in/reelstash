# Testing ReelStash

```bash
scripts/test.sh            # backend + UI
scripts/test.sh backend
scripts/test.sh app   # or admin
```

## Backend (`backend`)

`go test ./...` runs everything. Tests that need Postgres read `REELSTASH_TEST_DATABASE_URL` and
**skip when it is unset**. Point it at a scratch database, never one you use for real:

```bash
export REELSTASH_TEST_DATABASE_URL="postgres://reelstash:reelstash@localhost:5432/reelstash_test?sslmode=disable"
```

`scripts/test.sh` creates `reelstash_test` in the `reelstash-dev-postgres` container that `scripts/dev.sh` starts for you.

How the suite is organised:

| Area | Where | Notes |
| --- | --- | --- |
| HTTP API (routes, auth, CRUD, files, jobs, providers, streaming) | `internal/httpapi/*_test.go` | Drives the real router through `harness_test.go`: an admin and two users with live sessions. |
| Authorization matrix | `authz_test.go` | Walks every registered `/api` route: anonymous is 401, non-admins get 403 on admin routes. New routes are covered automatically. |
| UI ↔ server contract | `contract_test.go` | Reads `app/src/api.js` and `admin/src/api.js` and fails if it calls an endpoint the router does not have. |
| Sign-in | `internal/auth` | Runs the real OIDC + PKCE flow against an in-process provider (`internal/auth/authtest`). |
| Everything else | `internal/<pkg>/*_test.go` | scanner, namer, organizer, rename, jobs, notify, config, database, … |

Helpers in `internal/testdb`:

- `testdb.Shared(t)` – the shared schema. Namespace your rows (`uniq(...)`) and clean up after yourself.
- `testdb.Isolated(t)` – a throwaway schema, dropped after the test. **Use it** when a test writes global
  state (`app_setting`, provider config, organizer rules) or when it must see only its own rows, such as
  anything using the job runner, which consumes every PENDING job it can see.
- `testdb.Migrate(ctx, pool)` – use this instead of `database.Migrate`. Migrations replay triggers and
  functions, which deadlocks against other packages' tests when `go test` runs packages in parallel.
  It applies them once per change to the migration files.

## Web apps (`app`, `admin`)

- `npm run test:unit` – plain logic (`*.test.js`) on Node's built-in test runner.
- `npm run test:components` – React components, hooks and pages (`*.spec.jsx`) on Vitest + Testing Library
  in jsdom. `src/testing/render.jsx` wraps a screen in the router, theme and toaster it expects; mock
  `../api` with `vi.mock` to control what the server "returns".

Modules that import `./api` without a file extension (Vite style) can't be loaded by Node's runner;
write those as `*.spec.js` for Vitest.
