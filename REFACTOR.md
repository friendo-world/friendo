# Codebase Refactor

This document describes the restructuring of the Friendo codebase to support self-hosting as a first-class citizen, clarify the separation between CLI / runtimes / platform, and introduce the deploy/push/pull command model.

---

## Motivation

The original structure (`binary/`, `world/`) conflated several concerns:

- `binary/` was the CLI, the Go runtime, the admin UI, and the deploy client — all in one
- `world/` was the platform (friendo.world multi-tenancy) and the site runtime (template engine, data layer) — mixed together
- The deploy API lived in the platform, making self-hosting a second-class path
- "binary" and "world" describe implementation details, not what things do

The refactor separates **what the user types** (CLI) from **what runs a site** (runtimes) from **what provides managed hosting** (platform).

---

## Target structure

```
friendo/
├── cli/                            # The `friendo` command
│   └── cmd/friendo/
│       └── main.go                 # Cobra commands: init, serve, deploy, push, pull, export
│
├── runtime/
│   ├── go/                         # Go site runtime
│   │   ├── server/                 # HTTP server, routing, hot reload
│   │   ├── admin/                  # Admin UI routes + embedded Tailwind templates
│   │   │   ├── admin.go
│   │   │   ├── admin.css           # Tailwind input
│   │   │   └── static/admin.css    # Compiled, embedded
│   │   ├── api/                    # /_/api/* sync endpoints
│   │   │   └── api.go              # Push/pull handlers for templates, data, users, assets
│   │   ├── data/                   # SQLite layer
│   │   │   ├── data.go             # DB handle, queries
│   │   │   └── schema.sql          # Common schema (embedded)
│   │   ├── renderer/               # Pongo2 filters
│   │   │   └── renderer.go
│   │   ├── export/                 # Static HTML export
│   │   │   └── export.go
│   │   └── scaffold/               # `friendo init` templates
│   │       └── scaffold.go
│   │
│   └── edge/                       # JS site runtime (Cloudflare Workers)
│       ├── package.json
│       ├── wrangler.toml.example   # Template for self-hosters
│       ├── index.js                # Hono app: site serving + admin UI + sync API
│       └── schema.sql              # D1 schema (identical to Go runtime)
│
├── platform/                       # friendo.world (managed hosting)
│   ├── package.json
│   ├── wrangler.toml
│   ├── worker.js                   # WfP dispatch Worker: subdomain routing, platform auth, dashboard, provisioning
│   └── schema.sql                  # Platform-only tables (sites, platform users)
│
├── testsite/                       # Example site
│   ├── friendo.toml
│   ├── templates/
│   ├── pages/
│   └── public/
│
├── editor/                         # Phase 2 — desktop editor
├── go.mod                          # Single Go module at repo root
├── REFACTOR.md
├── PHASE_1.md
├── PHASE_1_5.md
├── DEVELOPMENT.md
└── README.md
```

---

## Key architectural changes

### 1. Workers for Platforms (WfP) for managed hosting

The platform uses [Cloudflare Workers for Platforms](https://developers.cloudflare.com/cloudflare-for-platforms/workers-for-platforms/) to run each site in its own isolated Worker with its own D1 database and R2 bucket.

**How it works:**

```
testsite.friendo.world
  → dispatch Worker (platform/worker.js)
    → extracts "testsite" from hostname
    → env.DISPATCHER.get("testsite")
  → user Worker (runtime/edge/index.js)
    → renders site, serves admin UI, handles sync API
    → has its own D1 + R2 bindings
```

The WfP architecture has three components:

| Component | What it is | Where it lives |
|---|---|---|
| **Dispatch Worker** | The platform entry point. Routes by subdomain, serves platform UI (landing page, dashboard), handles platform auth and provisioning. | `platform/worker.js` |
| **User Worker** | A per-site instance of the edge runtime. Handles site rendering, admin UI, and sync API. Each gets its own D1 database and R2 bucket. | `runtime/edge/index.js` (deployed as user Worker) |
| **Dispatch Namespace** | A single Cloudflare namespace containing all site Workers. | Managed via Cloudflare API |

**Two levels of D1:**

| Database | Owned by | Contains |
|---|---|---|
| **Platform D1** | Dispatch Worker | `sites` (registry), `user`, `session`, `verification` (Better Auth) |
| **Site D1** (one per site) | User Worker | `posts`, `users`, `sessions`, `comments`, `reactions`, etc. |

**Why WfP instead of a single shared Worker:**

- **True isolation.** Each site has its own D1 and R2 — no `site_id` partitioning, no shared database hotspot, no noisy neighbors.
- **Identical code paths.** Self-hosters deploy `runtime/edge/index.js` directly. The platform deploys the exact same code as a user Worker. Zero divergence.
- **Simpler schema.** The edge runtime doesn't need `site_id` columns at all in single-tenant mode.
- **Simpler platform Worker.** The dispatch Worker is just routing + platform UI + provisioning — no rendering logic.
- **Cloudflare manages lifecycle.** Worker deployment, scaling, and isolation are handled by the platform.

### 2. Sync API lives in the runtimes

Every Friendo site — regardless of where it runs — exposes the same sync endpoints:

```
POST /_/api/push/templates    # upload template files
POST /_/api/push/assets       # upload static assets
POST /_/api/push/data         # upsert records
POST /_/api/push/users        # upsert user accounts

GET  /_/api/pull/data          # fetch all records
GET  /_/api/pull/users         # fetch all user accounts
```

These endpoints are protected by site admin auth (email + password, same as the admin UI). The CLI authenticates against the site directly — no platform involvement.

Both `runtime/go/api/` and `runtime/edge/index.js` implement the same endpoints.

### 3. CLI commands: deploy, push, pull

**`friendo deploy`** — One-time interactive setup.

Provisions a site on a hosting target, saves the target URL to `friendo.toml`, then calls `friendo push` to upload everything.

```
$ friendo deploy

Where do you want to deploy?
  1. friendo.world (managed hosting)
  2. Cloudflare Workers (your own account)
  3. VPS / self-hosted
```

Each target runs a recipe:

| Target | What the recipe does |
|---|---|
| **friendo.world** | Authenticate with platform → provision site (creates D1 + R2 + user Worker via WfP API) → create site superadmin from local account → push |
| **Cloudflare** | Walk user through wrangler setup → create D1 + R2 → deploy `runtime/edge/` Worker → push |
| **VPS** | Give instructions to install the binary and start `friendo serve` → push |

After deploy, `friendo.toml` gains a deploy section:

```toml
[deploy]
target = "https://testsite.friendo.world"
```

**`friendo push`** — Push local state to the deployed site.

```bash
friendo push                # templates + static assets only
friendo push --data         # also push records
friendo push --users        # also push user accounts
```

Reads target from `friendo.toml`. Authenticates with site admin credentials (prompted on first push, cached in `~/.friendo/config`).

**`friendo pull`** — Pull remote state into local.

```bash
friendo pull --data         # pull records into local SQLite
friendo pull --users        # pull user accounts
```

### 4. Platform dispatch Worker

The platform (`platform/worker.js`) is a WfP dispatch Worker that handles:

**Bare domain requests** (`friendo.world`, `local.friendo.world`):
- Landing page
- Platform auth (Better Auth for platform accounts)
- Dashboard (list your sites, manage account)
- Provisioning API (`POST /api/sites` — creates D1 + R2 + deploys user Worker)
- CLI device auth flow

**Subdomain requests** (`{site}.friendo.world`):
- Look up site in platform D1
- Dispatch to user Worker via `env.DISPATCHER.get(siteId)`

The platform does NOT handle individual site rendering, admin UI, or sync — that's all in the user Worker (edge runtime).

**Platform D1 schema** (simplified — no more content tables):

```sql
-- Site registry
CREATE TABLE sites (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    subdomain   TEXT NOT NULL UNIQUE,
    owner_id    TEXT NOT NULL DEFAULT '',
    d1_id       TEXT NOT NULL DEFAULT '',  -- the site's D1 database ID
    r2_bucket   TEXT NOT NULL DEFAULT '',  -- the site's R2 bucket name
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Better Auth tables (platform accounts)
-- user, session, account, verification
```

### 5. Edge runtime becomes self-hostable

`runtime/edge/` is a standalone, single-site Cloudflare Worker. Self-hosters deploy it to their own Cloudflare account:

```bash
cd runtime/edge
cp wrangler.toml.example wrangler.toml  # configure D1, R2, domain
npx wrangler deploy
```

Then push their site to it:

```bash
cd my-site
friendo push --target https://mysite.com
```

The platform is just one of many possible deploy targets. The user Worker code on friendo.world is identical to what self-hosters run.

### 6. Local dev with `dev` dispatch namespace

Platform development uses a real WfP dispatch namespace called `dev`. The dispatch Worker runs locally via `wrangler dev` with `remote = true`, connecting to user Workers deployed in the `dev` namespace on Cloudflare. This gives full parity with production — same dispatch path, same per-site isolation.

```toml
# platform/wrangler.toml (local dev)
[[dispatch_namespaces]]
binding = "DISPATCHER"
namespace = "dev"
remote = true
```

```
local.friendo.world           → dispatch Worker (local) → platform UI
testsite.local.friendo.world  → dispatch Worker (local) → env.DISPATCHER.get("testsite") → user Worker (remote, dev namespace)
```

The `dev` namespace contains throwaway test sites, each with their own D1 and R2. Platform development requires a Cloudflare account and internet access.

**Two namespaces:**

| Namespace | Purpose | Used by |
|---|---|---|
| `dev` | Testing and development | `wrangler dev` (local) |
| `production` | Live friendo.world sites | `wrangler deploy --env production` |

**Site-only development** doesn't need the platform at all. The Go runtime on `localhost:3000` runs independently with a local SQLite database — no Cloudflare account, no internet required.

---

## Provisioning flow (friendo.world)

When a user runs `friendo deploy` and chooses friendo.world:

1. **CLI authenticates** with the platform via device auth flow
2. **CLI calls** `POST /api/sites` with site name + subdomain
3. **Platform provisions:**
   - Creates a new D1 database via Cloudflare API
   - Creates a new R2 bucket (or uses shared bucket with key prefixing)
   - Applies the edge runtime schema to the new D1
   - Deploys `runtime/edge/index.js` as a user Worker in the dispatch namespace, with bindings to the new D1 + R2
   - Records the site in the platform D1 (`sites` table)
4. **Platform returns** the site URL to the CLI
5. **CLI calls** `friendo push` to upload templates, assets, and optionally data/users to the new site's `/_/api/*` endpoints

---

## File migration plan (completed)

### Step 1: Move files (no logic changes) ✓

```
binary/cmd/friendo/main.go          → cli/cmd/friendo/main.go
binary/internal/server/server.go    → runtime/go/server/server.go
binary/internal/admin/              → runtime/go/admin/
binary/internal/data/               → runtime/go/data/
binary/internal/renderer/           → runtime/go/renderer/
binary/internal/export/             → runtime/go/export/
binary/internal/scaffold/           → runtime/go/scaffold/
binary/internal/deploy/             → cli/internal/deploy/
binary/go.mod                       → go.mod (repo root, Option A)

world/worker.js                     → platform/worker.js
world/schema.sql                    → platform/schema.sql
world/package.json                  → platform/package.json
world/wrangler.toml                 → platform/wrangler.toml
```

### Step 2: Extract sync API into runtimes ✓

New `runtime/go/api/api.go` with push/pull endpoints. Wired into the server.

### Step 3: Update CLI commands ✓

- `deploy` → interactive wizard with target selection
- `push` → push templates, assets, optionally data/users
- `pull` → pull data/users from remote
- `--sync-data` → `--data`, `--sync-users` → `--users`
- Target read from `friendo.toml`

### Step 4: Split Worker ✓

Extracted `runtime/edge/index.js` as standalone single-site Worker. Platform keeps multi-tenant routing, Better Auth, dashboard, provisioning.

### Step 5: Update docs and build scripts ✓

### Step 6: Reshape platform as WfP dispatch Worker ✓

- Stripped all site rendering, admin UI, and sync code from `platform/worker.js`
- Added dispatch logic: `env.DISPATCHER.get(siteId)` for subdomain requests
- Simplified platform schema to sites registry + Better Auth tables only
- Added provisioning endpoint with TODO for Cloudflare API calls (D1 + R2 + user Worker creation)
- Updated `wrangler.toml` with dispatch namespace bindings (`dev` for local, `production` for deploy)
- Removed `bcryptjs` dependency (site auth lives in user Workers now)

### Step 7: Clean up site_id ✓

- Edge runtime reads `SITE_ID` from env with `"default"` fallback
- Removed vestigial `sites` table from Go runtime schema (platform concern only)
- Go runtime keeps `site_id = "local"` for local SQLite (consistent with existing data)

### Step 8: Update CLI provisioning ✓

- Split client into `PlatformClient` (provisioning via `/api/sites`) and `SiteClient` (sync via `/_/api/*`)
- `friendo deploy` uses `PlatformClient` for provisioning, then `SiteClient` for push
- `friendo push` / `friendo pull` talk directly to the site's sync API — no platform involvement
- Files sent as JSON to `/_/api/push/templates` and `/_/api/push/assets` (not individual uploads)

## Open TODOs

- ~~**Provisioning automation:**~~ Done. `POST /api/sites` now creates D1, applies the edge runtime schema, creates an R2 bucket, and deploys the user Worker to the dispatch namespace via Cloudflare API. Requires `CF_ACCOUNT_ID` and `CF_API_TOKEN` secrets, and the edge runtime bundle + schema in the `RUNTIME_BUCKET` R2 bucket.
- **Site admin auth for CLI:** `friendo push` needs to authenticate with the site's admin (email + password), get a session cookie, and cache it. Currently stubbed with an empty cookie.
- **D1 migrations:** The edge runtime should check schema version on first request and apply pending migrations. See REFACTOR.md architecture section for the design.

---

## What this does NOT change

- The template engine (Pongo2 locally, custom JS on edge)
- The common schema (identical SQLite/D1 tables everywhere)
- The content type system
- The admin UI design or functionality
- The Phase 2 editor plan
- Better Auth for platform accounts (friendo.world only)
