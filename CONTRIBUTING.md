# Contributing to ReelStash

Thanks for helping out. ReelStash is licensed under the [GNU GPL v3](LICENSE); by contributing you agree your changes are released under the same license.

## Before you start

- For anything larger than a small fix, open an issue first so we can agree on the approach.
- Report security problems privately, not as issues. See [SECURITY.md](SECURITY.md).

## Setup

You need Docker, Go (the version in `backend/go.mod`) and Node.js 22 with npm.

```bash
npm ci --prefix app
npm ci --prefix admin
scripts/dev.sh
```

The app runs at <http://localhost:3000> and the admin at <http://localhost:3001/admin>. More detail: [Getting started](docs/getting-started.md) and [Development](docs/development.md).

## Making a change

1. Fork the repository and branch from `main`.
2. Keep each pull request focused on one change.
3. Add or update tests. Backend tests that need Postgres use a scratch database (see [TESTING.md](TESTING.md)); never point them at data you care about.
4. Run the tests and make sure they pass:

   ```bash
   scripts/test.sh
   ```

5. Update the docs in `docs/` when you change behavior, configuration, or the API.
6. Open a pull request that explains what changed and why.

## Layout

- `backend/`: Go API, workers, providers, SQL migrations
- `app/`: media library UI (React/Vite)
- `admin/`: Admin, Files and Downloads UI (React/Vite)
- `docs/`: product and developer documentation
- `mobile/`: Android app

Database changes go in a new embedded migration; never edit one that has shipped.
