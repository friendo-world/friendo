# Development

## Prerequisites

- [Go 1.21+](https://go.dev/dl/)
- [Node.js](https://nodejs.org/)
- [Wrangler CLI](https://developers.cloudflare.com/workers/wrangler/install-and-update/) (`npm i -g wrangler`) — for edge/platform development
- Cloudflare tunnel (`cloudflared`) — optional, for local subdomain routing via HTTPS

## Project structure

```
friendo/
├── cli/                  # The `friendo` CLI command
│   ├── cmd/friendo/      # Main entry point
│   └── internal/deploy/  # Deploy client (push/pull to remote)
├── admin/                # Shared admin UI — one SPA, served by BOTH runtimes
│   ├── src/              # Preact + TypeScript app
│   ├── scripts/          # bundle-edge.mjs (mirrors the build into the edge)
│   └── vite.config.ts    # builds to runtime/go/admin/spa
├── runtime/
│   ├── go/               # Go site runtime (local dev server)
│   │   ├── server/       # HTTP server, routing, hot reload
│   │   ├── admin/        # Serves the embedded admin SPA bundle
│   │   │   └── spa/      # Built admin SPA (embedded via go:embed)
│   │   ├── api/          # /_/api/* REST + sync endpoints (auth, content, users, push/pull)
│   │   ├── data/         # SQLite layer + schema
│   │   ├── renderer/     # Pongo2 filters
│   │   ├── export/       # Static HTML export
│   │   └── scaffold/     # `friendo init` templates
│   └── edge/             # JS site runtime (Cloudflare Workers)
│       ├── index.js      # Hono app: site serving + admin SPA + REST/sync API
│       ├── spa-bundle.js # Built admin SPA, embedded (generated)
│       └── schema.sql    # D1 schema (identical to Go runtime)
├── platform/             # friendo.world (managed hosting, WfP dispatch Worker)
│   ├── worker.js         # Dispatch Worker: subdomain routing, platform auth, provisioning
│   └── schema.sql        # Platform-only tables (sites registry, Better Auth)
├── testsite/             # Example site for local development
├── go.mod                # Single Go module at repo root
└── package.json          # Dev scripts
```

## Quick start

There are three dev environments, depending on what you're working on:

### 1. Go runtime (site development)

The default for day-to-day site work. No Cloudflare account or internet needed.

```bash
npm run serve           # build + serve testsite on :3000
npm run serve:open      # same but skip admin auth
```

| URL | What |
|---|---|
| `http://localhost:3000` | Local site |
| `http://localhost:3000/_/` | Local admin UI |
| `http://localhost:3000/_/api/pull/data` | Local sync API |

### 2. Edge runtime (Workers development)

For developing `runtime/edge/index.js` — the template engine, admin UI, and sync API that runs on Cloudflare. Uses miniflare for local D1 + R2. No Cloudflare account needed.

```bash
npm run edge:install    # first time only
npm run edge:dev        # start edge runtime on :8788
```

The edge runtime applies its D1 schema automatically on the first request (see
[migrations.js](runtime/edge/migrations.js)), so there's no separate schema step.
`npm run edge:schema` still exists if you want to apply it manually.

Then push your testsite to it:
```bash
cd testsite && friendo push --target http://localhost:8788
```

| URL | What |
|---|---|
| `http://localhost:8788` | Edge site (local) |
| `http://localhost:8788/_/` | Edge admin UI (local) |
| `http://localhost:8788/_/api/pull/data` | Edge sync API (local) |

### 3. Platform (dispatch + managed hosting)

For developing `platform/worker.js` — the dispatch Worker, platform UI, provisioning. Requires a Cloudflare account and internet — dispatches to real user Workers in the remote `dev` namespace.

```bash
npm run platform:install  # first time only
npm run platform:dev      # start dispatch Worker on :8787
npm run tunnel            # map *.local.friendo.world → :8787
```

| URL | What |
|---|---|
| `https://local.friendo.world` | Platform landing / dashboard |
| `https://testsite.local.friendo.world` | Dispatched to user Worker in `dev` namespace |

### Full dev environment

`npm run dev` starts everything together: Go binary on `:3000`, platform Worker on `:8787`, and the Cloudflare tunnel.

## CLI commands

| Command | Description |
|---|---|
| `friendo init [name]` | Scaffold a new site |
| `friendo serve` | Start the local dev server (compiles `content/`, hot reload) |
| `friendo build` | Compile the `content/` folder (markdown) into the site database |
| `friendo export` | Export as static HTML or bundle |
| `friendo deploy` | Interactive deploy wizard (`--api-url` to target a non-default platform) |
| `friendo redeploy` | Re-push the current managed-hosting runtime to your site's Worker |
| `friendo destroy` | Deprovision the site (deletes its Worker, D1, and R2; `--yes` to skip confirm) |
| `friendo push` | Push templates + assets to deployed site |
| `friendo push --data` | Also push records |
| `friendo push --users` | Also push user accounts |
| `friendo pull --data` | Pull records from deployed site |
| `friendo pull --users` | Pull user accounts |

## Scripts

| Command | Description |
|---|---|
| `npm run build` | Build admin SPA + Go binary |
| `npm run admin:install` | Install admin SPA deps (first time only) |
| `npm run admin` | Build the admin SPA (→ `runtime/go/admin/spa` + edge bundle) |
| `npm run admin:dev` | Vite dev server for the admin SPA (hot reload) |
| `npm test` | Run the parity tests against both runtimes |
| `npm run test:go` | Parity tests, Go runtime only (fast) |
| `npm run test:edge` | Parity tests, edge runtime only (boots wrangler) |
| `npm run serve` | Build + serve testsite on :3000 (Go runtime) |
| `npm run serve:open` | Same but skip admin auth |
| `npm run edge:dev` | Start edge runtime on :8788 (miniflare D1 + R2) |
| `npm run edge:schema` | Apply schema to local edge D1 |
| `npm run runtime:bundle` | Bundle `runtime/edge/` → `dist/edge-runtime.js` (the per-site Worker) |
| `npm run runtime:publish` | Publish the bundle to the `friendo-runtime` R2 bucket (provisioning reads it) |
| `npm run platform:dev` | Start platform dispatch Worker on :8787 |
| `npm run tunnel` | Map *.local.friendo.world → :8787 |
| `npm run dev` | Full dev environment (Go + platform + tunnel) |
| `npm run clean` | Remove build artifacts |

## Testing

[tests/](tests/) holds a **parity harness**: one shared `scenarios.json` of
request→assert steps, run against both the Go and edge runtimes, asserting they
behave identically. `npm test` runs both; `npm run test:go` is a fast
Cloudflare-free subset. To extend coverage, add a step to `scenarios.json` and it
runs against both runtimes automatically — see [tests/README.md](tests/README.md).

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design — the two runtimes, the
shared admin SPA, the REST/sync API, Workers for Platforms, and migrations. The
dev environments you'll actually run:

| Environment | What you're testing | Internet required? | Port |
|---|---|---|---|
| **Go runtime** | Site templates, data, admin UI (Go) | No | `:3000` |
| **Edge runtime** | Site templates, data, admin UI (Workers) | No | `:8788` |
| **Platform** | Dispatch, provisioning, platform UI | Yes (remote `dev` namespace) | `:8787` |

The Go and edge runtimes use fully local storage (SQLite / miniflare D1+R2). The platform dispatches to real user Workers in a remote `dev` namespace on Cloudflare — full parity with production.

## Admin UI

The admin UI is a single Preact SPA in [admin/](admin/), built once and served
**identically by both runtimes** at `/_/` — so the UI never drifts between Go and
edge. The runtimes differ only in their REST handlers, not in the interface.

```
admin/  ──vite build──▶  runtime/go/admin/spa/   (go:embed)
                    └──▶  runtime/edge/spa-bundle.js  (generated, imported by the Worker)
```

The SPA talks to a runtime-agnostic REST API under `/_/api/*`. Both runtimes
implement it (`runtime/go/api/`, `runtime/edge/index.js`). To work on the UI:

```bash
npm run admin:install   # first time only
npm run admin:dev       # Vite dev server with hot reload
# point its API calls at a running runtime, e.g. `npm run serve` on :3000
```

`npm run build` rebuilds the SPA before compiling, so the embedded bundle stays
in sync. The entire admin UI — including first-run setup/migrate — is the SPA;
the runtimes only serve the bundle and the REST API.

## Auth

**Local admin:** Create a superadmin account (email + password) on first run at `/_/setup`.

**Edge site admin:** Same auth model — site-level users/sessions in the site's own D1. Push local users to edge with `friendo push --users`.

**Platform:** Better Auth at `local.friendo.world/login`. Separate from site auth. Used for managing your platform account and provisioning sites.

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

Custom filters (implemented identically in both runtimes):

- `{{ "img.jpg"|asset_url }}` → `/assets/img.jpg`
- `{{ post.created|date:"Jan 2, 2006" }}` — format a date
- `{{ "img.jpg"|asset_url|resize:"300x200" }}` → `/assets/img.jpg?w=300&h=200` — a resize *hint* (`W`, `WxH`, or `xH`). An image CDN like Cloudflare Image Resizing honors `w`/`h`; the built-in static server ignores them and serves the original.

## Tailwind

The admin SPA ([admin/](admin/)) uses Tailwind CSS via the `@tailwindcss/vite`
plugin — it's compiled into the SPA bundle by `npm run admin`, no separate step.
Iterate on it with `npm run admin:dev` (Vite hot reload). The public site's own
styles are plain CSS authored by the site owner; Friendo doesn't impose Tailwind there.
