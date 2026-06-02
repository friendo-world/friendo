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
├── runtime/
│   ├── go/               # Go site runtime (local dev server)
│   │   ├── server/       # HTTP server, routing, hot reload
│   │   ├── admin/        # Admin UI routes + embedded Tailwind templates
│   │   ├── api/          # /_/api/* sync endpoints (push/pull)
│   │   ├── data/         # SQLite layer + schema
│   │   ├── renderer/     # Pongo2 filters
│   │   ├── export/       # Static HTML export
│   │   └── scaffold/     # `friendo init` templates
│   └── edge/             # JS site runtime (Cloudflare Workers)
│       ├── index.js      # Hono app: site serving + admin UI + sync API
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
npm run edge:schema     # apply schema to local D1
npm run edge:dev        # start edge runtime on :8788
```

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
| `friendo serve` | Start the local dev server |
| `friendo export` | Export as static HTML or bundle |
| `friendo deploy` | Interactive deploy wizard |
| `friendo push` | Push templates + assets to deployed site |
| `friendo push --data` | Also push records |
| `friendo push --users` | Also push user accounts |
| `friendo pull --data` | Pull records from deployed site |
| `friendo pull --users` | Pull user accounts |

## Scripts

| Command | Description |
|---|---|
| `npm run build` | Compile Tailwind CSS + Go binary |
| `npm run css` | Compile Tailwind CSS only |
| `npm run css:watch` | Watch mode for Tailwind CSS |
| `npm run serve` | Build + serve testsite on :3000 (Go runtime) |
| `npm run serve:open` | Same but skip admin auth |
| `npm run edge:dev` | Start edge runtime on :8788 (miniflare D1 + R2) |
| `npm run edge:schema` | Apply schema to local edge D1 |
| `npm run platform:dev` | Start platform dispatch Worker on :8787 |
| `npm run tunnel` | Map *.local.friendo.world → :8787 |
| `npm run dev` | Full dev environment (Go + platform + tunnel) |
| `npm run clean` | Remove build artifacts |

## Architecture

Friendo uses [Cloudflare Workers for Platforms](https://developers.cloudflare.com/cloudflare-for-platforms/workers-for-platforms/) for managed hosting on friendo.world. Each deployed site runs as an isolated user Worker with its own D1 database and R2 bucket.

**Production** (friendo.world):
```
testsite.friendo.world/blog/hello
  → Dispatch Worker (platform/worker.js)
    → looks up "testsite" in platform D1
    → env.DISPATCHER.get("testsite")
  → User Worker (runtime/edge/index.js)
    → renders the page from its own D1 + R2
```

**Two levels of D1:**

| Database | Owned by | Contains |
|---|---|---|
| Platform D1 | Dispatch Worker | Sites registry, platform user accounts (Better Auth) |
| Site D1 (per site) | User Worker | Posts, users, sessions, comments — all site data |

Self-hosters deploy `runtime/edge/index.js` directly — the same code, without the dispatch layer.

### Dev environments

| Environment | What you're testing | Internet required? | Port |
|---|---|---|---|
| **Go runtime** | Site templates, data, admin UI (Go) | No | `:3000` |
| **Edge runtime** | Site templates, data, admin UI (Workers) | No | `:8788` |
| **Platform** | Dispatch, provisioning, platform UI | Yes (remote `dev` namespace) | `:8787` |

The Go and edge runtimes use fully local storage (SQLite / miniflare D1+R2). The platform dispatches to real user Workers in a remote `dev` namespace on Cloudflare — full parity with production.

See the **Quick start** section above for setup commands.

## Auth

**Local admin:** Create a superadmin account (email + password) on first run at `/_/setup`.

**Edge site admin:** Same auth model — site-level users/sessions in the site's own D1. Push local users to edge with `friendo push --users`.

**Platform:** Better Auth at `local.friendo.world/login`. Separate from site auth. Used for managing your platform account and provisioning sites.

## Sync API

Every Friendo site (local or deployed) exposes the same sync endpoints at `/_/api/*`:

```
POST /_/api/push/templates    # upload template files
POST /_/api/push/assets       # upload static assets
POST /_/api/push/data         # upsert records
POST /_/api/push/users        # upsert user accounts

GET  /_/api/pull/data          # fetch all records
GET  /_/api/pull/users         # fetch all user accounts
```

All endpoints require site admin auth (session cookie from `/_/login`).

## Templates

[Pongo2](https://github.com/flosch/pongo2) (Jinja2-compatible). Available context:

- `{{ site.name }}` — from `friendo.toml`
- `{{ request.path }}` — current URL path
- `{{ collections.blog }}` — all posts in a collection
- `{{ record }}` — matched record on dynamic routes (e.g. `pages/blog/[slug].html`)

## Tailwind

Admin UI uses Tailwind CSS, compiled from `runtime/go/admin/admin.css` and embedded in the Go binary via `go:embed`. The Tailwind config scans `admin.go` for class usage via `@source "./admin.go"`.

To iterate on styles:
```bash
npm run css:watch   # in one terminal
npm run serve       # in another
```
