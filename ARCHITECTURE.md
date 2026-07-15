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

A **site is a folder**: `friendo.toml`, `layouts/`, `pages/`, `assets/`, and a
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
| Assets | `assets/` on disk (uploads land in `assets/uploads/`) | R2 (same key layout) |
| Templates | [Pongo2](https://github.com/flosch/pongo2) (Jinja2) | a custom Jinja2-compatible engine (no `eval`, Workers-safe) |
| Realtime | in-process message hub (SSE) | a `ChannelStream` Durable Object (SSE) |

The client transport is identical either way — `<friendo-channel>` opens one
`EventSource` on `/channels/:id/stream` and new messages stream in live.

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

Access is **capability-based**, not a fixed admin gate (see *Auth model* below):
each route requires a capability, and content/comment routes additionally scope to
the actor's *own* rows unless they hold the `.any` variant.

| Area | Endpoints | Capability |
|---|---|---|
| Bootstrap | `GET /me`, `POST /auth/login`, `POST /auth/logout`, `GET/POST /setup`, `POST /migrate`, `POST /auth/request-code`, `POST /auth/verify-code` | public |
| Personas | `GET/POST /me/personas`, `POST /me/personas/:id/default` — an account's author profiles + which one attribution uses | authenticated member (own personas) |
| Content | `GET /collections`, CRUD under `/collections/:c/records` and `/records/:id`; `GET /records`, `PUT /records/:id/status` (review queue) | `content.create` (own); `content.edit.any` / `content.publish` for others' posts + publishing |
| Community | `GET/POST /posts/:id/comments`, `PUT/DELETE /comments/:id` (moderation); `POST/DELETE /reactions`; `GET/POST /polls`, `POST /polls/:id/vote` | authenticated baseline (comment/react/vote); `comment.moderate.own/any` to moderate; `content.edit.any` to author a poll |
| Channels (realtime) | `GET/POST /channels`, `DELETE /channels/:id`; `GET/POST /channels/:id/messages`, `DELETE /messages/:id`; `GET /channels/:id/stream` (SSE) | read/post = authenticated member; channel mgmt = `site.configure` |
| Locations | `GET /locations` (public); `POST /locations`, `DELETE /locations/:id` | read public; write = `content.edit.any` |
| Media | `GET /files` (public); `POST /files` (multipart image → `assets/`), `DELETE /files/:id` | read public; write = `content.create` |
| Users | `GET/POST /users`, `PUT/DELETE /users/:id` (role rules + last-owner guard) | `user.manage`; granting admin/owner needs `site.own` |
| Settings | `GET/PUT /settings` (site name + `access.*` / `content.require_approval` policy) | `site.configure` |
| Sync | `POST /push/{templates,assets,data,users,settings,files}`, `GET /pull/{data,users,settings,files}` | `site.configure` |

**Community data renders two ways.** The `friendo.js` SDK components fetch these
endpoints client-side (interactive, viewer-aware). For content that benefits from a
no-JS read, both runtimes *also* attach the public relations to the focused record
in the template context — `record.comments` (approved), `record.reactions`,
`record.poll`, and `record.gallery` — so a post template can render them directly.
Both engines attach them at the same point in the render path, so output is
byte-identical (covered by the render-parity suite). Inherently interactive features
(realtime chat, the `<friendo-map>` widget) stay client-side by design.

## Deploy, push, pull

The CLI talks to a site's own `/_/api/*` — no platform involvement, whatever the
host (friendo.world, your Cloudflare account, or a VPS).

- **`friendo deploy`** — one-time interactive setup. Provisions the site on a
  chosen target, writes `[deploy] target = …` to `friendo.toml`, then pushes.
- **`friendo push`** — upload templates + assets (`--data` / `--users` to include
  records and accounts). Assets are base64-encoded so binary files (uploaded
  images in `assets/uploads/`) round-trip intact, and `--data` also carries
  `files` rows so a deployed site's media metadata matches the source.
- **`friendo pull`** — fetch remote records/accounts (and media rows) back into
  the local database.

**Auth + first-run bootstrap.** `push`/`pull` authenticate via `authenticateSite()`:
reuse a cached session from `~/.friendo/config`, else log in. If the target has
**no admin yet** (a freshly provisioned site), `GET /_/api/setup` reports it and
the CLI walks the user through creating the first admin via `POST /_/api/setup` —
so the deploy wizard's final push bootstraps the site owner automatically.

## File-based content (`content/`)

Content normally lives in the DB (edited via the admin UI or API). A site can
*also* be authored as **files** — a Hugo-style `content/` folder that compiles
into that same DB, so the runtimes are untouched and parity holds.

- `content/<collection>/<slug>.md` → a record in `<collection>` with that slug;
  YAML front matter supplies `title`/`slug`/`status`/`date`, and **any other keys
  land in a `data` JSON column** (migration `0003`), readable in templates as
  `record.data.<field>`. The markdown body is stored verbatim and rendered by the
  `markdown` filter at template time.
- The importer (`runtime/go/content`) is a Go/CLI-side layer. `friendo build` runs
  it; `friendo serve` runs it on startup and re-imports on every `content/` edit
  (hot reload); `friendo push`/`deploy` run it before uploading. Upserts by
  `(collection, slug)`, so `content/` is the source of truth when present.

This documentation site is authored this way (`docs/content/docs/*.md`), with its
sidebar generated from the docs collection via `collections.docs|sort_by:"data.weight"`.

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

**Runtime artifacts.** The provisioner deploys a *bundled* edge runtime, not the
source tree. `npm run runtime:bundle` esbuild-bundles `runtime/edge/index.js`
(hono + bcryptjs + marked inlined, `.sql` files loaded as text) into
`runtime/edge/dist/edge-runtime.js`; `npm run runtime:publish` uploads it to the
`friendo-runtime` R2 bucket (`RUNTIME_BUCKET`). Re-run these whenever
`runtime/edge/` changes so new sites get the current runtime. (No schema artifact
is shipped — the schema is embedded in the bundle and self-applies; see below.)

**Tenant DNS is one wildcard.** A single proxied `*.friendo.world` DNS record
(created once) means every subdomain resolves instantly to Cloudflare's edge,
where the `*.friendo.world/*` route hands it to the dispatch Worker. So
provisioning creates **no per-site DNS** — a brand-new subdomain is reachable the
moment its Worker is live, with no propagation wait. Universal SSL covers
`*.friendo.world`, so HTTPS is automatic.

**Provisioning** (`friendo deploy` → friendo.world): the CLI authenticates with
the platform (device-auth flow), then `POST /api/sites` creates a **D1 database +
R2 bucket + user Worker** (the bundled runtime from `RUNTIME_BUCKET`, bound as
`DB`, `ASSETS`, `SITE_ID`, `SITE_NAME`) and records the resource IDs in the
platform registry — no DNS, no schema pre-apply (the site's D1 self-initializes
from the embedded baseline on its first request). It's ~3 seconds. Any partial
failure rolls back the resources it created. The CLI then pushes (and bootstraps
the admin) over the new site's `/_/api/*`. Re-running `friendo deploy` for an
existing site (or `POST /api/sites/:id/redeploy`) re-pushes the current bundle to
its user Worker with the same bindings, so runtime updates reach live sites
without touching their data. `DELETE /api/sites/:id` deprovisions a site — tearing
down its user Worker, D1, and R2 bucket (emptying it first) — then removes the
registry row.

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
| **Site** | Per-site **accounts** (`users`) with roles (owner > admin > editor > contributor > member), bcrypt passwords, DB-stored sessions. Identical model in both runtimes. | site D1 / SQLite |
| **Platform** | friendo.world account auth (Better Auth) for managing your account and provisioning sites — independent of site auth. | platform D1 |

**Capabilities, not just ranks.** Authorization is capability-based: roles are
named bundles of capabilities (`content.create`, `content.edit.own` vs
`content.edit.any`, `comment.moderate.own` vs `comment.moderate.any`,
`user.manage`, `site.configure`, `site.own`). The **ownership axis** (own vs. any)
lets a Contributor edit only what they authored while an Editor edits anything —
something a pure rank ladder can't express. Per-site policy (`access.default_role`,
`access.signups_enabled`, `content.require_approval`) is stored in `site_settings`
and surfaced as one-click presets (Personal / Community / Blog) in the admin UI.
The public render path shows only `published` posts. Full design in
[design/auth-permissions.md](design/auth-permissions.md).

**Accounts vs. profiles (personas).** A `users` row is an *account* (the auth
identity). Display **profiles** live in `authors`, linked by `authors.user_id` — one
account can have several. All content (`posts`, `comments`, …) references a profile
via `author_id` → `authors.id`; every account gets a default profile on creation. A
member manages their profiles as **personas** (`/_/api/me/personas`) and picks which
one attribution uses; the choice is a pointer, `users.default_author_id` (migration
`0010`), honored by `DefaultAuthorID` — so switching persona re-attributes all their
comments/messages with no per-write plumbing. The `<friendo-auth>` component exposes
the switcher.

**Members** are visitors who verify their email via a one-time code
(`/auth/request-code` → `/auth/verify-code`, backed by `otp_codes`) to get a
passwordless `member` account. They share the same session/cookie and role gate,
so they authenticate but can't reach admin endpoints. See the Phase 3 design in
[docs/content/docs/phase-3-community.md](docs/content/docs/phase-3-community.md)
(also published at `docs.friendo.world/docs/phase-3-community`).

Bcrypt hashes are portable, so `friendo push --users` carries accounts to a
deployed site unchanged — the same password works everywhere.
