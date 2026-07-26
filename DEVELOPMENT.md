# Development

## Prerequisites

- [Go 1.21+](https://go.dev/dl/) — the only requirement to build and run friendo
- [Node.js](https://nodejs.org/) — optional; only to rebuild the admin SPA or `sdk/friendo.js`. Both are committed, so running friendo needs no Node build step.

## Project structure

```
friendo/
├── cli/                  # The `friendo` CLI command
│   ├── cmd/friendo/      # Main entry point
│   └── internal/deploy/  # Deploy client (push/pull to remote)
├── admin/                # Admin UI — one Preact SPA, served by the Go runtime
│   ├── src/              # Preact + TypeScript app
│   └── vite.config.ts    # builds to runtime/go/admin/spa (committed)
├── sdk/                  # friendo.js — community Web Components, served at /friendo.js
│   └── friendo.js        #   (auth/comments/reactions/poll/channel/map; committed)
├── runtime/
│   └── go/               # Go runtime — THE runtime (local dev = self-host = friendo.world)
│       ├── server/       # HTTP server, routing, hot reload (`friendo serve` — one site)
│       ├── network/      # Network mode (`friendo network serve` — many sites by subdomain):
│       │                 #   dispatcher, site registry, operator console, account auth
│       ├── storage/      # Media storage — R2/S3 (FRIENDO_S3_*) or local disk (default)
│       ├── admin/        # Serves the embedded admin SPA bundle
│       │   └── spa/      # Built admin SPA (committed, embedded via go:embed)
│       ├── api/          # /_/api/* REST + sync endpoints (auth, content, users, push/pull)
│       ├── data/         # SQLite layer + schema
│       ├── email/        # Email delivery (Resend) for OTP sign-in
│       ├── renderer/     # Pongo2 filters
│       ├── export/       # Static HTML export
│       └── scaffold/     # `friendo init` templates
├── testsite/             # Example site for local development
├── tests/                # Go regression suite (REST scenarios, template render, SQL splitter) + SDK browser check
├── go.mod                # Single Go module at repo root
└── package.json          # Dev scripts
```

## Quick start

The whole product is **one binary, the Go runtime** (`runtime/go/`). Local dev,
self-host, and friendo.world are the same binary — there's no separate hosting
runtime to run. Build it with plain Go, no Node step:

```bash
go build ./cli/cmd/friendo   # the admin SPA + sdk/friendo.js are committed
```

### 1. Go runtime (site development)

The default for day-to-day work. No account or internet needed — fully local.

```bash
npm run serve           # build + serve testsite on :3000
npm run serve:open      # same but skip admin auth
```

| URL | What |
|---|---|
| `http://localhost:3000` | Local site |
| `http://localhost:3000/_/` | Local admin UI |
| `http://localhost:3000/_/api/pull/data` | Local sync API |

The binary has **two run modes**: `friendo serve` hosts one site (above), and
`friendo network serve` hosts many sites by subdomain — the network mode that
runs friendo.world. See [DEPLOY.md](DEPLOY.md) and the network mode below.

### 2. Network mode (many sites, one operator)

`friendo network serve` is the same binary hosting many tenant sites by
subdomain, each its own folder + database, with an operator console at the apex.
It's what friendo.world runs; you can run it locally too:

```bash
friendo network serve --base-domain localhost   # sites at <subdomain>.localhost:3000
```

| URL | What |
|---|---|
| `http://localhost:3000` | Operator console (apex) + device-auth for the CLI |
| `http://demo.localhost:3000` | The `demo` tenant site |

Media offloads to R2/S3 when `FRIENDO_S3_*` is set (disk by default). See
[DEPLOY.md](DEPLOY.md) for the full network deploy (Coolify + Cloudflare + R2).

## CLI commands

| Command | Description |
|---|---|
| `friendo init [name]` | Scaffold a new site |
| `friendo serve` | Start the local dev server (compiles `content/`, hot reload) |
| `friendo build` | Compile the `content/` folder (markdown) into the site database |
| `friendo export` | Export as static HTML or bundle |
| `friendo deploy [subdomain] [--network URL]` | Publish this folder to a friendo network (friendo.world by default): browser device-auth → claim the subdomain → push |
| `friendo login [url]` | Sign in to a network in your browser (device auth; default friendo.world) |
| `friendo whoami [url]` | Show the account you're signed in as |
| `friendo logout` | Sign out — clear the cached token and site sessions |
| `friendo push` | Push templates + assets to deployed site |
| `friendo push --data` | Also push records |
| `friendo push --users` | Also push user accounts |
| `friendo pull --data` | Pull records from deployed site |
| `friendo pull --users` | Pull user accounts |

### `friendo network *` — run or manage a network

The network group operates on a network you run on this box (`--root`, default
`./network`, or `FRIENDO_NETWORK_ROOT`) or on a remote network (`--network URL`,
after `friendo login`).

| Command | Description |
|---|---|
| `friendo network serve` | Serve every site on the network by subdomain (the friendo.world dev server) |
| `friendo network sites` | List the sites on the network |
| `friendo network provision <subdomain>` | Create a new site |
| `friendo network deploy <subdomain> --network URL` | Provision + push this folder in one command |
| `friendo network destroy <subdomain> --yes` | Delete a site and all its data (irreversible) |
| `friendo network invite <email>` | Pre-create an account so they can sign in when signups are invite-only |
| `friendo network signups <open\|invite>` | Set who may create an account (anyone / operator-invited) |
| `friendo network operator grant <email>` | Grant an account the operator capability (on-box) |

## Scripts

| Command | Description |
|---|---|
| `npm run build` | Build admin SPA + SDK + Go binary |
| `npm run admin:install` | Install admin SPA deps (first time only) |
| `npm run admin` | Build the admin SPA (→ `runtime/go/admin/spa`) |
| `npm run admin:dev` | Vite dev server for the admin SPA (hot reload) |
| `npm run sdk` | Bundle `sdk/friendo.js` (→ `runtime/go/sdk/friendo.js`, served at `/friendo.js`) |
| `npm test` / `npm run test:go` | The Go test suite (`go test ./...`) — the primary test command |
| `npm run test:sdk` | Playwright browser check of the `friendo.js` Web Components |
| `npm run check` | `go vet` + `go build` + `npm test` |
| `npm run serve` | Build + serve testsite on :3000 (Go runtime) |
| `npm run serve:open` | Same but skip admin auth |
| `npm run network` | Build + run a local network — `friendo network serve` on :3000 (sites at `<subdomain>.localhost`) |
| `npm run dev` | Build + run a local `friendo network serve` on :3000 with dev OTP echo (`bash dev.sh`) |
| `npm run init` | Build + scaffold a new site (`friendo init`) |
| `npm run export` | Build + export testsite as static HTML |
| `npm run deploy:dry` | Build + dry-run `friendo push` for testsite |
| `npm run release -- v0.2.0` | Cut a release: guard + bundle-check + `go test` + tag/push. Add `--dry-run` to stop before anything irreversible |
| `npm run clean` | Remove build artifacts |

## Testing

The **Go test suite is primary** — `go test ./...` (or `npm run test:go`) covers
the runtime, network mode, and API. A **CLI push/pull round-trip** lives in
`cli/internal/deploy/roundtrip_test.go`.

`npm run test:sdk` is a separate **browser check** (Playwright): it boots the Go
runtime over a throwaway site and drives Chromium to confirm the `friendo.js` Web
Components render — `<friendo-map>` paints its markers and the `<friendo-auth>`
persona switcher works. It needs network (the map loads Leaflet + OSM tiles from a
CDN).

[tests/](tests/) holds the **Go regression suite** — shared fixtures
(`scenarios.json` REST steps, `render-scenarios.json` golden renders,
`splitter-cases.json` SQL splitting) exercised in-process against the Go runtime
as part of `go test ./...`. See [tests/README.md](tests/README.md).

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design — the Go runtime, its
two run modes, the admin SPA, the REST/sync API, and migrations. What you'll
actually run:

| Mode | What you're testing | Internet required? | Port |
|---|---|---|---|
| **`friendo serve`** | Site templates, data, admin UI (one site) | No | `:3000` |
| **`friendo network serve`** | Subdomain dispatch, operator console, provisioning (many sites) | No | `:3000` |

Both are the same Go binary against local storage (SQLite on disk; media on disk
by default, or R2/S3 when `FRIENDO_S3_*` is set). For hosting a live network, see
[DEPLOY.md](DEPLOY.md).

## Admin UI

The admin UI is a single Preact SPA in [admin/](admin/), built once and embedded
into the Go binary, served at `/_/`. The built bundle is committed under
`runtime/go/admin/spa/`, so running friendo needs no Node step.

```
admin/  ──vite build──▶  runtime/go/admin/spa/   (go:embed, committed)
```

The SPA talks to the REST API under `/_/api/*`, served by the Go runtime
(`runtime/go/api/`). To work on the UI:

```bash
npm run admin:install   # first time only
npm run admin:dev       # Vite dev server with hot reload
# point its API calls at a running runtime, e.g. `npm run serve` on :3000
```

`npm run build` rebuilds the SPA before compiling, so the embedded bundle stays
in sync. The entire admin UI — including first-run setup/migrate — is the SPA;
the runtime only serves the bundle and the REST API.

## Auth

**Site owner:** Create the site owner account (email + password) on first run at
`/_/setup`. This is unchanged — a single site's admin still logs in with a
password, and `friendo push --users` moves users to a deployed site.

**Network account:** Signing in to a network is **passwordless** — email OTP for
the operator console, plus **device auth** for the CLI (`friendo login`). Being an
**operator** is a capability on an account, not a separate login. Bootstrapping:

- `FRIENDO_OPERATOR_EMAIL` grants the first operator on boot; otherwise the first
  sign-in at the apex console claims it.
- `RESEND_API_KEY` + `FRIENDO_EMAIL_FROM` deliver the OTP by email on a live
  network. In local dev without them, `FRIENDO_OTP_ECHO` prints the code to the
  server log instead (auto-enabled by `friendo serve` when no email is configured).

See [DEPLOY.md](DEPLOY.md) for the full network auth + env setup.

## REST + sync API

Every Friendo site (local or deployed) exposes the same API at `/_/api/*`, backing
both the admin SPA and the CLI. The sync endpoints used by `friendo push`/`pull`:

```
POST /_/api/push/templates    # upload template files
POST /_/api/push/assets       # upload static assets
POST /_/api/push/data         # upsert records
POST /_/api/push/users        # upsert user accounts

GET  /_/api/pull/data          # fetch all records
GET  /_/api/pull/users         # fetch all user accounts
```

Most endpoints require site admin auth (a `friendo_session` cookie from
`POST /_/api/auth/login`); the bootstrap endpoints (`/me`, `/auth/*`, `/setup`,
`/migrate`) are public. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full
endpoint surface.

## Templates

[Pongo2](https://github.com/flosch/pongo2) (Jinja2-compatible). Available context:

- `{{ site.name }}` — from `friendo.toml`
- `{{ request.path }}` — current URL path
- `{{ collections.blog }}` — all posts in a collection
- `{{ record }}` — matched record on dynamic routes (e.g. `pages/blog/[slug].html`)

Custom filters:

- `{{ "img.jpg"|asset_url }}` → `/assets/img.jpg`
- `{{ post.created|date:"Jan 2, 2006" }}` — format a date
- `{{ "img.jpg"|asset_url|resize:"300x200" }}` → `/assets/img.jpg?w=300&h=200` — a resize *hint* (`W`, `WxH`, or `xH`). An image CDN in front of the site can honor `w`/`h`; the built-in server ignores them and serves the original.

## Tailwind

The admin SPA ([admin/](admin/)) uses Tailwind CSS via the `@tailwindcss/vite`
plugin — it's compiled into the SPA bundle by `npm run admin`, no separate step.
Iterate on it with `npm run admin:dev` (Vite hot reload). The public site's own
styles are plain CSS authored by the site owner; Friendo doesn't impose Tailwind there.
