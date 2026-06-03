# Phase 1.5 — In Progress

Phase 1.5 hardens the foundation and makes Friendo production-ready. Four tracks: auth, UI, interactivity, and architecture.

## Completed

### Auth overhaul (M1)
- `admin` table replaced by `users` + `sessions` + `otp_codes` tables
- Setup requires email + password (creates superadmin)
- Full user management: list, create, edit, delete users with role hierarchy
- Roles enforced on all routes: superadmin > admin > editor > member
- Session tokens stored in DB with expiry, validated on every request
- Migration path for existing Phase 1 sites (legacy admin → superadmin)
- Site-level auth on the edge (bcrypt + sessions in D1, same as local)
- Platform auth (Better Auth) kept separate for friendo.world accounts

### Design system (M2)
- Tailwind CSS compiled and embedded in the Go binary (~13KB)
- All admin templates converted to Tailwind utility classes
- Served at `/_/static/admin.css` — no CDN, no external dependency
- `npm run css` / `npm run css:watch` for development

### Admin UI rebuild (M3 — superseded by the shared SPA)
An interim step first moved the Go admin templates out of string constants into
`embed.FS` files (settings page, richer dashboard, responsive nav). That work has
since been **superseded**: the two runtimes had divergent admin UIs (Go templates
vs. edge JS-strings), so the admin UI is being rebuilt as a single shared SPA
served identically by both runtimes. See **Shared admin SPA** below.

### Codebase refactor (M5)
- `binary/` → `cli/` + `runtime/go/`
- `world/` → `platform/` + `runtime/edge/`
- Single Go module at repo root (`github.com/henryholtgeerts/friendo`)
- Sync API (`/_/api/*` push/pull endpoints) extracted into both runtimes
- `deploy` → interactive wizard with target selection (friendo.world, Cloudflare, VPS)
- `push` / `pull` as ongoing sync verbs with `--data` / `--users` flags
- Edge runtime (`runtime/edge/index.js`) extracted as standalone single-site Worker
- Self-hosting is now a first-class path — identical code to friendo.world
- `go build ./...` passes against the new layout

### Workers for Platforms integration (M6)
- `platform/worker.js` reshaped as a WfP dispatch Worker — subdomain routing via `env.DISPATCHER.get(siteId)`
- All site rendering / admin UI / sync code stripped from the platform
- Platform is now: subdomain routing, landing page, dashboard, platform auth (Better Auth), provisioning API
- Provisioning (`POST /api/sites`) calls the Cloudflare API to create per-site D1, apply the edge schema, create R2, and deploy the user Worker (`createD1Database`, `applyD1Schema`, `deployUserWorker`)
- Platform D1 schema simplified to sites registry + Better Auth tables only
- Each site on friendo.world runs as an isolated user Worker (`runtime/edge/index.js`)
- CLI `friendo deploy` uses the provisioning API via `PlatformClient`
- See [REFACTOR.md](./REFACTOR.md) for full architecture details

### Shared admin SPA + REST API (M3 + M4)
The admin UI is now one Preact SPA in [admin/](admin/), built once and served
byte-for-byte identically by both runtimes (`go:embed` for Go, an embedded bundle
for the edge). Both runtimes implement the same runtime-agnostic REST API under
`/_/api/*`, so the UI never diverges. Every endpoint was verified on the Go runtime
and on the edge under miniflare with identical JSON shapes and status codes.

- **Foundations:** `admin/` Vite + Preact + TS; build → `runtime/go/admin/spa` + `runtime/edge/spa-bundle.js` (identical asset hashes). Auth: `GET /me`, `POST /auth/login`, `POST /auth/logout`.
- **Records:** `GET /collections`, full CRUD under `/collections/:c/records` and `/records/:id`; SPA dashboard, collection list, record form (preact-iso routing).
- **Users:** list/create/update/delete under `/users` with server-enforced role rules (superadmin > admin > editor > member); SPA users list + form.
- **Settings:** `GET /settings` (site name + counts); SPA settings view.
- **First-run:** `GET/POST /setup` and `POST /migrate`; SPA bootstraps into Setup/Migrate/Login based on `/setup` status. Fresh-site setup verified end-to-end.
- **Cleanup:** removed the Go server-rendered admin (templates, embedded Tailwind, `npm run css`) and the edge's old JS-string admin — the SPA is the single source of truth.

**Remaining stretch (original M4):** `data-friendo-*` progressive-enhancement
attributes for public-site comments/reactions/polls (not required by the admin UI).

### CLI ↔ site admin auth (deploy/push/pull now working)
The blocker is resolved — the `friendo push` / `pull` / `deploy` workflow is
functional end-to-end.
- `friendo push` / `pull` authenticate via `authenticateSite()` ([auth.go](cli/internal/deploy/auth.go)): reuse a cached session, else branch on the site's state
- **Existing site:** prompt for the admin's email + password → `POST /_/api/auth/login` (no-echo via `golang.org/x/term`)
- **Fresh/empty site (e.g. just provisioned on friendo.world):** `GET /_/api/setup` reports no admin → walk the user through creating the first admin account → `POST /_/api/setup`. This closes the managed-hosting bootstrap gap — the deploy wizard's final push creates the site admin automatically.
- `SiteClient` captures the `friendo_session` cookie from login/setup ([client.go](cli/internal/deploy/client.go)); the session is cached per-target in `~/.friendo/config` (`Config.Sites`), revalidated via `GET /_/api/me`, and re-prompted when expired
- Verified end-to-end against a running Go runtime: login path (bad password rejected, push/pull reuse cached session, pulled records land locally) **and** the first-run path (push to an empty site creates the superadmin via setup, then uploads)

## Remaining

### Smaller loose ends (done)
- **D1 migrations:** the edge runtime applies pending migrations once per isolate on the first request ([migrations.js](runtime/edge/migrations.js) + an `ensureMigrated` guard in [index.js](runtime/edge/index.js)). Migrations are tracked in a `schema_migrations` table; the baseline migration is the schema itself, so a fresh D1 self-initializes on first request — no manual `wrangler d1 execute` needed. Verified end-to-end against a wiped miniflare D1.
- **Image `resize` filter:** implemented in both runtimes ([renderer.go](runtime/go/renderer/renderer.go) + the edge `applyFilter`) as a resize-*hint* — `|resize:"WxH"` appends `?w=…&h=…` to the URL for an image CDN (e.g. Cloudflare Image Resizing) to honor; the built-in static server ignores them and serves the original, so it degrades gracefully. Verified identical output on Go and edge.
