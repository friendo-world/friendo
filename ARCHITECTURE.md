# Architecture

How Friendo is designed and why. For setup and day-to-day commands, see
[DEVELOPMENT.md](DEVELOPMENT.md); for what's shipped and what's next, see
[ROADMAP.md](ROADMAP.md).

## The three pieces

Friendo separates **what you type** from **what runs a site** from **what
provides managed hosting**:

| Piece | What it is | Where |
|---|---|---|
| **CLI** | The `friendo` command — init, serve, deploy, push, pull, export. A thin client that talks to any running site over its REST API. | `cli/` |
| **Runtimes** | The thing that actually runs a site: serves pages, stores data, hosts the admin UI and API. Two implementations (Go and edge), same behavior. | `runtime/go/`, `runtime/edge/` |
| **Platform** | friendo.world managed hosting. Optional — everything it does, the runtimes can do on their own. | `platform/` |

A **site is a folder**: `friendo.toml`, `templates/`, `pages/`, `public/`, and a
`data/` database. It's portable, checkable into git, and outlives any single tool.

## Two runtimes, one site

Every Friendo site — wherever it runs — exposes the same interface:

- `/` — the site (templates + data)
- `/_/` — the admin UI
- `/_/api/*` — the REST + sync API

There are two runtimes that implement it:

| | Go runtime (`runtime/go/`) | Edge runtime (`runtime/edge/`) |
|---|---|---|
| Runs as | A single Go binary (`friendo serve`), e.g. on a VPS | A Cloudflare Worker (Hono app) |
| Data | SQLite (`modernc.org/sqlite`, no CGO) | D1 |
| Assets | `public/` on disk | R2 |
| Templates | [Pongo2](https://github.com/flosch/pongo2) (Jinja2) | a custom Jinja2-compatible engine (no `eval`, Workers-safe) |

**Common schema.** Both use identical table definitions (`posts`, `comments`,
`reactions`, `channels`, `messages`, `polls`, `poll_votes`, `authors`,
`locations`, `files`, `users`, `sessions`). Identical schema everywhere is what
makes data sync a copy, not a migration.

**Parity is the rule.** Self-hosters deploy the exact `runtime/edge/` code that
friendo.world runs as a per-site Worker — zero divergence between managed and
self-hosted.

## The admin UI: one SPA, both runtimes

The admin UI is a single Preact + TypeScript app in `admin/`, built once and
served **byte-for-byte identically** by both runtimes, so it can never drift:

```
admin/  ──vite build──▶  runtime/go/admin/spa/      (embedded via go:embed)
                    └──▶  runtime/edge/spa-bundle.js  (generated, imported by the Worker)
```

The SPA is pure client-side; it talks only to the runtime-agnostic REST API. The
runtimes differ in their handlers, never in the interface.

## REST + sync API (`/_/api/*`)

Both runtimes implement the same endpoints. Most require site admin auth (a
`friendo_session` cookie); the bootstrap endpoints are public so the SPA and CLI
can start cold.

| Area | Endpoints | Auth |
|---|---|---|
| Bootstrap | `GET /me`, `POST /auth/login`, `POST /auth/logout`, `GET/POST /setup`, `POST /migrate`, `POST /auth/request-code`, `POST /auth/verify-code` | public |
| Content | `GET /collections`, CRUD under `/collections/:c/records` and `/records/:id` | admin+ |
| Users | `GET/POST /users`, `PUT/DELETE /users/:id` (role rules enforced) | admin+ |
| Settings | `GET /settings` | admin+ |
| Sync | `POST /push/{templates,assets,data,users}`, `GET /pull/{data,users}` | admin+ |

## Deploy, push, pull

The CLI talks to a site's own `/_/api/*` — no platform involvement, whatever the
host (friendo.world, your Cloudflare account, or a VPS).

- **`friendo deploy`** — one-time interactive setup. Provisions the site on a
  chosen target, writes `[deploy] target = …` to `friendo.toml`, then pushes.
- **`friendo push`** — upload templates + assets (`--data` / `--users` to include
  records and accounts).
- **`friendo pull`** — fetch remote records/accounts back into the local database.

**Auth + first-run bootstrap.** `push`/`pull` authenticate via `authenticateSite()`:
reuse a cached session from `~/.friendo/config`, else log in. If the target has
**no admin yet** (a freshly provisioned site), `GET /_/api/setup` reports it and
the CLI walks the user through creating the first admin via `POST /_/api/setup` —
so the deploy wizard's final push bootstraps the site owner automatically.

## Managed hosting: Workers for Platforms

friendo.world runs each site as its own isolated Worker via
[Cloudflare Workers for Platforms](https://developers.cloudflare.com/cloudflare-for-platforms/workers-for-platforms/):

```
testsite.friendo.world
  → dispatch Worker (platform/worker.js)       — extracts "testsite" from the hostname
    → env.DISPATCHER.get("testsite")
  → user Worker (runtime/edge/index.js)         — renders the site from its own D1 + R2
```

| Component | Role |
|---|---|
| **Dispatch Worker** (`platform/worker.js`) | Subdomain routing, platform landing/dashboard, platform auth (Better Auth), provisioning. No rendering. |
| **User Worker** (`runtime/edge/index.js`) | A per-site instance of the edge runtime — rendering, admin UI, API. Its own D1 + R2. |
| **Dispatch namespace** | A Cloudflare namespace holding all site Workers (`dev` for development, `production` for live). |

**Two levels of D1:**

| Database | Owned by | Contains |
|---|---|---|
| Platform D1 | Dispatch Worker | `sites` registry + platform accounts (Better Auth) |
| Site D1 (one per site) | User Worker | All site content + users + sessions |

True isolation: each site has its own D1 and R2 — no `site_id` partitioning, no
shared hotspot, no noisy neighbors.

**Provisioning** (`friendo deploy` → friendo.world): the CLI authenticates with
the platform (device-auth flow), then `POST /api/sites` creates a D1 database and
R2 bucket, applies the schema, deploys `runtime/edge/index.js` as a user Worker
bound to them, and records the site in the platform registry. The CLI then pushes
(and bootstraps the admin) over the new site's `/_/api/*`.

## Schema migrations

Both runtimes share a migration mechanism: an ordered list whose baseline (id 1)
is the schema itself, tracked in a `schema_migrations` table. The Go runtime
applies pending migrations in `data.Open`; the edge runtime applies them once per
isolate on the first request (guarded by `ensureMigrated`). A freshly provisioned
database self-initializes from the baseline. Add a migration by dropping the same
`NNNN_*.sql` file in **both** runtime trees (`runtime/go/data/migrations/` and
`runtime/edge/migrations/`) — the schema stays identical across SQLite and D1.

## Auth model

| Scope | What | Where |
|---|---|---|
| **Site** | Per-site **accounts** (`users`) with roles (superadmin > admin > editor > member), bcrypt passwords, DB-stored sessions. Identical model in both runtimes. | site D1 / SQLite |
| **Platform** | friendo.world account auth (Better Auth) for managing your account and provisioning sites — independent of site auth. | platform D1 |

**Accounts vs. profiles.** A `users` row is an *account* (the auth identity).
Display **profiles** live in `authors`, linked by `authors.user_id` — one account
can have several. All content (`posts`, `comments`, …) references a profile via
`author_id` → `authors.id`; every account gets a default profile on creation.

**Members** are visitors who verify their email via a one-time code
(`/auth/request-code` → `/auth/verify-code`, backed by `otp_codes`) to get a
passwordless `member` account. They share the same session/cookie and role gate,
so they authenticate but can't reach admin endpoints. See
[docs/phase-3-community.md](docs/phase-3-community.md).

Bcrypt hashes are portable, so `friendo push --users` carries accounts to a
deployed site unchanged — the same password works everywhere.
