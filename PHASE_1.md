# Friendo — Phase 1 implementation plan

Phase 1 delivers the two things that make Friendo real: a binary you can build a site with locally, and a deploy command that puts it on the internet. Everything else — the editor, the community features — builds on this foundation.

**Scope:** Friendo binary + CLI + Friendo.world (Cloudflare edge runtime + deploy pipeline)  
**Output:** A developer can `friendo init`, build a site, and `friendo deploy` it to a live URL.

---

## Tech stack

| Layer | Technology | Role |
|---|---|---|
| Runtime language | Go | Binary compilation, HTTP server, CLI |
| Database | SQLite (via modernc.org/sqlite) | Embedded database, pure Go, no CGO |
| HTTP router | chi | Lightweight, stdlib-compatible router |
| Template engine | Pongo2 | SSR template rendering (Jinja2-compatible) |
| Edge runtime | Cloudflare Workers + Hono | Serves templates in production, deploy API |
| Edge database | Cloudflare D1 | Shared SQLite-compatible database for all sites |
| Asset storage | Cloudflare R2 | Media and static file hosting |
| Edge auth | Better Auth + Kysely | Email + password, browser-based device flow for CLI |
| Edge renderer | Custom (Jinja2-subset) | Lightweight Workers-safe template engine, no eval() |

---

## Content type architecture

Friendo uses a **common schema** across all sites — both locally (SQLite) and on the edge (D1). Rather than allowing arbitrary user-defined collections, Friendo ships opinionated content types that cover common use cases.

### Why a common schema?

The original plan was to let users define arbitrary database collections and mirror each site's unique schema to its own D1 database at deploy time. This created several problems:

1. **Migration complexity** — every deploy required diffing a local SQLite schema against remote D1 and generating safe DDL.
2. **Operational overhead** — thousands of D1 databases to manage with no unified view.
3. **Relational data** — a generic `records (data_json)` table makes JOINs and relational queries impractical.
4. **Phase 3 blocker** — cross-site community features require cross-database queries, which D1 doesn't support.

A common schema solves all four: deploy becomes a data sync (not a schema migration), one D1 database serves all sites, relational queries use real foreign keys, and cross-site features are just queries with different `site_id` values.

### Base schema

```sql
-- Core
sites           (id, name, subdomain, owner_id, config_json, created, updated)

-- Content
posts           (id, site_id, collection, slug, title, body, author_id, status, published_at, created, updated)
comments        (id, site_id, post_id, parent_id, author_id, body, created, updated)
reactions       (id, site_id, target_type, target_id, author_id, emoji, created)

-- Community
channels        (id, site_id, name, kind[chat|forum|feed], created, updated)
messages        (id, site_id, channel_id, author_id, body, parent_id, created, updated)
polls           (id, site_id, post_id, question, closes_at, created)
poll_votes      (id, poll_id, option_index, author_id, created)

-- People
authors         (id, site_id, name, email, avatar, role, created, updated)

-- Geo
locations       (id, site_id, target_type, target_id, lat, lng, label, created)

-- Media
files           (id, site_id, record_type, record_id, field, r2_key, mime, size, created)
```

**Design conventions:**
- `site_id` on every table — partitions all data by site.
- `posts.collection` is a string (`"blog"`, `"pages"`, `"recipes"`) — one table supports multiple content types. `{{ collections.blog }}` queries `posts WHERE collection = 'blog'`.
- `target_type` + `target_id` (on reactions, locations) — polymorphic references for cross-cutting features.
- `parent_id` (on comments, messages) — enables threading/nesting.
- `channels.kind` — one table covers chat rooms, forums, and feeds.

### Expandability

New content types are added by adding new tables in future releases. Examples:

```sql
-- Events / Calendars (future)
events          (id, site_id, title, description, starts_at, ends_at, recurrence, location_id)
rsvps           (id, event_id, author_id, status[going|maybe|declined])

-- Matchmaking / Profiles (future)
profiles        (id, site_id, author_id, bio, preferences_json, visibility)
matches         (id, site_id, profile_a, profile_b, status[pending|accepted|declined])
```

Rule of thumb: **structured relationships get real columns, freeform user data gets JSON** (e.g. `preferences_json` on profiles, `config_json` on sites).

### Admin UI

Friendo provides its own admin interface at `/_/` built with htmx and server-rendered HTML (no JS framework, no build step, ships in the binary via Go's `embed` package). The admin UI:

- Shows which content types are enabled for the site (configured via `friendo.toml`)
- Lets you browse and edit records within those types
- Manages file uploads
- Does not expose schema creation or modification

---

## Auth strategy

Auth operates in three separate contexts:

**Local admin UI (`/_/`):**
- First-run password, stored hashed in `data/friendo.db`
- No OAuth, no session infrastructure — it's your machine
- `friendo serve --open-admin` skips auth entirely for local dev

**CLI deploy (`friendo deploy`):**
- Browser-based device flow — CLI generates a code, opens browser to `friendo.world/cli/auth`
- User signs in (or signs up) with email + password via Better Auth
- CLI polls until the browser confirms, receives a session token
- Token stored in `~/.friendo/config`, reused for subsequent deploys until expiry

**Site visitors on friendo.world (future):**
- Anonymous-first — visitors participate with a display name, no login required
- Optional identity upgrade via email
- `authors` table stores site-level identities
- Auth logic lives entirely in the Worker — the binary never handles production auth

This matches the "no accounts required" philosophy: building doesn't need an account, visiting doesn't need an account, deploying uses a simple email + password sign-up.

---

## Project structure

```
friendo/
├── binary/
│   ├── cmd/
│   │   └── friendo/
│   │       └── main.go              # CLI entrypoint
│   └── internal/
│       ├── server/                  # HTTP server and routing
│       ├── renderer/                # Pongo2 rendering + filter registry
│       ├── data/                    # SQLite database + schema setup
│       ├── admin/                   # Friendo admin UI (htmx, embedded)
│       ├── deploy/                  # Deploy pipeline (API client)
│       ├── export/                  # Static + bundle export
│       └── scaffold/                # friendo init logic
├── world/
│   ├── worker.js                    # Cloudflare Worker (Hono + Better Auth)
│   ├── schema.sql                   # Common schema for D1 (+ auth tables)
│   ├── wrangler.toml                # Worker config (dev + production envs)
│   └── sync.sh                      # Dev tool: sync local site to D1/R2
└── testsite/                        # Example site for development
```

---

## Milestones

### M1 — Bare binary ✓

**Goal:** `friendo serve` renders a Pongo2 template from a local directory.

**Deliverables:**
- Go HTTP server reads `.html` files from a `/pages` directory
- Pongo2 renders templates with a basic context object (`{{ site.name }}`, `{{ request.path }}`)
- Template inheritance working (`{% extends %}`, `{% block %}`)
- `friendo serve` command starts the server on `localhost:3000`
- `--port` flag supported

---

### M2 — Data layer ✓

**Goal:** Database embedded. Collections queryable from templates. Dynamic routing working.

**Deliverables:**
- SQLite database initialised in `/data/` on first run
- Collections available in template context as `{{ collections.blog }}`, etc.
- File-based dynamic routing: `pages/blog/[slug].html` resolves `/blog/my-post` against a collection
- Custom Pongo2 filters registered: `asset_url`, `date`, `resize`
- Custom 404 page support via `pages/404.html`

**Note:** M2 was built with PocketBase for rapid prototyping. M2.5 replaces it with direct SQLite and the common schema.

---

### M2.5 — Common schema + admin UI ✓

**Goal:** Drop PocketBase. Use direct SQLite with the fixed common schema. Ship the Friendo admin UI.

**Deliverables:**
- PocketBase removed as a dependency; replaced with `modernc.org/sqlite` (pure Go) and `chi` router
- Common schema SQL file (`schema.sql`) applied automatically on first run
- Data package rewritten to query fixed tables (`posts`, `comments`, etc.)
- `friendo.toml` controls which content types are active for a site
- Template context populated from fixed tables: `{{ collections.blog }}` queries `posts WHERE collection = 'blog'`
- Friendo admin UI at `/_/` (htmx + server-rendered HTML, embedded via Go `embed`)
- Admin UI supports: browsing records by content type, creating/editing/deleting records, file uploads
- First-run admin password set on initial visit to `/_/`

**Done when:** A developer can enable `posts` in `friendo.toml`, create a blog post through the Friendo admin UI, and see it rendered in a template. PocketBase is no longer a dependency.

---

### M3 — Full CLI ✓

**Goal:** All four CLI commands working. Scaffold and developer experience polished.

**Deliverables:**

`friendo init [name]`
- Creates a new site directory with canonical structure (`/templates`, `/pages`, `/public`, `/data`)
- Generates a `friendo.toml` with sensible defaults
- Creates a starter `templates/base.html` and `pages/index.html`
- Prints a friendly getting-started message

`friendo serve`
- Hot reload: template changes reflected without restart
- Clear error output when a template fails to render (line number, template name)
- Graceful handling of missing content types (warn, don't crash)

`friendo export`
- Mode 1 (static): renders all pages to HTML files in `/dist` — no server required to host
- Mode 2 (bundle): packages site directory + binary into a self-contained archive

`friendo deploy` *(stub only at this milestone — wired up in M5)*
- Authenticates with Friendo.world (email OTP flow, token stored in `~/.friendo/config`)
- Prints a clear error if the edge runtime isn't ready yet

**Done when:** A developer can go from zero to a running local site in under two minutes using only the CLI.

---

### M4 — Edge runtime ✓

**Goal:** The Cloudflare Worker + D1 + R2 stack works. A single Worker serves all Friendo sites via subdomain routing. A site can be manually deployed and served from the edge with behavior matching local exactly.

**Deliverables:**

Multi-tenant subdomain routing:
- A single Cloudflare Worker (Hono) handles all requests to `*.friendo.world`
- The Worker extracts `site_id` from the hostname: `my-site.friendo.world` → `site_id = "my-site"`
- Local dev uses `?site=name` query param to simulate subdomains
- All D1 queries are filtered by `site_id` — one database, many sites
- Each site's templates are stored in R2 under a prefix: `sites/{site_id}/pages/...`, `sites/{site_id}/templates/...`

Worker (`world/worker.js`):
- Hono router with three concerns: auth, deploy API, and site rendering
- Better Auth handles user registration and sign-in (email + password)
- Browser-based device flow for CLI authentication (`/cli/auth`)
- Deploy API endpoints for site registration, record sync, and asset upload
- Custom Jinja2-compatible template engine (no `eval()`, Workers-safe)
- Routing logic mirrors file-based routing from the binary (including `[slug]` dynamic routes)
- Template context (`collections`, `record`, `site`, `request`) built identically to the local binary
- Template engine supports recursive `{% extends %}`, `{% for %}`, `{% if %}`, filters
- `asset_url` and `date` filters produce identical output to local Pongo2
- R2 assets served directly for `/public/*` requests
- Returns a proper 404 page if the site doesn't exist or the route doesn't match

Common schema on D1:
- `world/schema.sql` applied to a D1 database — extends the local schema with Better Auth tables (`user`, `session`, `account`, `verification`)
- `sites` table holds site metadata with `owner_id`; `site_id` on every content table partitions data
- No translation layer needed between local and cloud

Wrangler configuration (`world/wrangler.toml`):
- Environment-based: default for local dev (miniflare), `production` for Cloudflare
- D1 binding for the shared database
- R2 binding for template and asset storage
- Route pattern: `*.friendo.world/*` (production env)

Manual deploy verified end-to-end:
- `sync.sh` applies schema, registers site, syncs records to D1, uploads templates/assets to R2
- Verified locally via `wrangler dev` + `?site=` param
- Site rendering, dynamic routes, 404s, and collections all working

**Done when:** A developer can follow a manual checklist and have a working cloud version of their local site at `{name}.friendo.world`, served by the same Worker that serves every other Friendo site.

---

### M5 — `friendo deploy`

**Goal:** The full deploy pipeline is automated behind a single command.

**Deliverables:**

`friendo deploy` pipeline (in order):
1. Authenticate via browser-based device flow (open browser → sign in → CLI receives token)
2. Reuse token from `~/.friendo/config` on subsequent deploys
3. Register site if new — reserve `{name}.friendo.world` subdomain via `POST /api/sites`
4. Sync local records to D1 via `POST /api/sites/:id/sync` (upsert by ID, all tagged with `site_id`)
5. Upload templates and public assets to R2 via `POST /api/sites/:id/assets`
6. Print live URL on success

Note: the Worker is already deployed and serving all sites (from M4). Deploy does NOT bundle or upload a Worker — it only syncs data, templates, and assets via the Worker's API.

Additional:
- Re-deploy is idempotent — running `friendo deploy` twice is safe
- `--dry-run` flag prints what would be deployed without doing it
- Custom domain support via `friendo.toml`:
  ```toml
  [deploy]
  domain = "mysite.com"
  ```
- Meaningful error messages at each step (auth failure, subdomain taken, upload failure)

**Done when:** `friendo deploy` takes a fresh site from local to live in a single command with no manual steps.

---

## Friendo.world API (built into the Worker)

The deploy API lives in the same Worker that serves sites — no separate backend needed. Auth is handled by Better Auth, and all endpoints require a valid session token in the `Authorization` header.

**Endpoints:**

| Method | Path | Description |
|---|---|---|
| `*` | `/api/auth/*` | Better Auth (sign-up, sign-in, session management) |
| `GET` | `/cli/auth?code=XXX` | Device flow auth page (opened by CLI) |
| `POST` | `/cli/auth/complete` | Device flow completion (browser → CLI) |
| `GET` | `/api/cli/poll?code=XXX` | CLI polls for device flow token |
| `POST` | `/api/sites` | Register a new site, reserve subdomain |
| `POST` | `/api/sites/:id/sync` | Accept record data, upsert into D1 |
| `POST` | `/api/sites/:id/assets` | Accept asset uploads, sync to R2 |
| `GET` | `/api/sites/:id` | Site status |

Because all sites share a common D1 schema, the sync endpoint is a simple upsert — no schema migration logic required.

---

## Key technical decisions

**Direct SQLite over PocketBase**  
PocketBase was used for M2 prototyping but its value diminished as the architecture shifted to a common schema. PocketBase's strengths — arbitrary schema management, admin UI, REST API — were all being bypassed or replaced. Dropping it in favor of `modernc.org/sqlite` (pure Go, no CGO) + `chi` (stdlib-compatible router) gives Friendo full control over the data model, a smaller binary, and fewer dependencies.

**Common schema over arbitrary collections**  
Letting users define arbitrary schemas locally created an impedance mismatch with the cloud. A common schema means: deploy is data sync (not schema migration), one D1 database serves all sites, relational queries use real foreign keys, and cross-site features (Phase 3) are just queries with different `site_id` values. The tradeoff — users can't create arbitrary tables — is acceptable because the built-in content types cover the vast majority of use cases.

**Better Auth with browser-based device flow**  
No third-party accounts required anywhere in the Friendo ecosystem. Developers authenticate for deploy via email + password in the browser — the CLI opens a browser window (like `gh auth login`), the user signs in, and the CLI receives a session token. Better Auth handles user management, session tokens, and password hashing. Site visitors participate anonymously by default and can optionally upgrade to a persistent identity. Auth logic lives entirely in the friendo.world Worker — the local binary never handles production auth.

**Custom template engine for the edge renderer**  
Cloudflare Workers block `eval()` and `new Function()`, which rules out Nunjucks and most JS template engines. Rather than precompiling templates (complex deploy pipeline) or using WASM (1MB limit), Friendo uses a lightweight custom renderer (~200 lines) that implements the Jinja2 subset used by Pongo2: `extends` (recursive), `block`, `for`/`else`/`endfor`, `if`/`elif`/`else`/`endif`, `set`, `include`, `raw`, variable interpolation, dot access, and filters. Zero dependencies, Workers-safe, output matches the local binary exactly — verified with identical `date` filter (Go format strings) and `asset_url` paths.

**Static export as a first-class feature**  
`friendo export --static` is not an afterthought. It's the escape hatch that makes Friendo's portability promise credible. Some sites have no dynamic content and should deploy to Cloudflare Pages or Netlify as a plain HTML folder. Friendo supports this from day one.

**Content types are expandable by design**  
New content types (events, profiles, commerce) are added in future releases by adding new tables. Existing sites and data are never affected. The schema conventions — `site_id` partitioning, polymorphic references, JSON columns for freeform data — are designed to support this growth.

---

## Open questions

- **Rate limits and quotas.** What are the free tier limits on Friendo.world? (Worker requests, D1 reads, R2 storage.) Needs a decision before public launch.
- **Pongo2 filter parity.** The `resize` filter implies server-side image processing. In the cloud context this means a Worker that proxies R2 assets through image transformation (Cloudflare Images, or a simple sharp-based resize Worker). Scope for Phase 1 or defer to Phase 2?
- **D1 sharding strategy.** One D1 database works for early scale. At what point do we shard by region or site volume? Not urgent, but worth a napkin estimate before public launch.

## Resolved decisions

- **Auth model:** Developer deploy via Better Auth (email + password, browser-based device flow). Site visitors anonymous-first with optional email upgrade. Local admin via first-run password or `--open-admin`.
- **Admin UI technology:** htmx + server-rendered HTML, embedded via Go `embed`. No JS framework, no build step.
- **Database layer:** Direct SQLite (`modernc.org/sqlite`) replaces PocketBase. See key technical decisions above.
- **Edge framework:** Hono for routing + Better Auth + Kysely for D1 access. Single Worker serves all sites and the deploy API.
- **Project layout:** `world/` at repo root (not nested in `binary/edge/`). The Worker is a separate JS project, not part of the Go binary.

---

## Definition of done for Phase 1

Phase 1 is complete when a developer with no prior Friendo experience can:

1. Install the binary
2. Run `friendo init my-site`
3. Run `friendo serve` and see their site at `localhost:3000`
4. Edit a template and see the change reflected without restarting
5. Create a blog post through the Friendo admin UI and see it appear in the rendered page
6. Run `friendo deploy` and receive a live URL at `[name].friendo.world`

That loop — init, build, deploy — is the heartbeat of the project. Everything in Phase 1 serves it.
