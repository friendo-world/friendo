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

### Admin UI rebuild (M3)
- Admin templates moved out of Go string constants into `embed.FS` files under [runtime/go/admin/templates/](runtime/go/admin/templates/) — one `.html` per page, parsed once at startup
- Shared `nav.html` partial replaces the navbar that was duplicated across six pages, with active-tab highlighting
- New **settings page** (`/_/settings`) — site name, collection/user counts, current account
- Richer **dashboard** — per-user greeting and live record counts per collection
- Responsive layout — `flex-wrap` nav, `sm:` breakpoints, horizontally scrollable tables
- Tailwind `@source` repointed from `admin.go` to `templates/` (kept `admin.go` in scope for the role-tag colors emitted from Go)

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

## Remaining

### CLI ↔ site admin auth (blocker)
The deploy/push/pull workflow is structurally complete but **non-functional** until this lands.
- `/_/api/*` enforces site admin auth — returns 401 without a valid session ([api.go](runtime/go/api/api.go))
- The CLI always builds its sync client with an empty cookie: `NewSiteClient(target, "")` in [deploy.go](cli/internal/deploy/deploy.go) (two `TODO`s)
- Needed: prompt for site admin credentials → `POST /_/login` → capture the `friendo_session` cookie → cache in `~/.friendo/config` → pass through to `SiteClient`
- Both ends already exist (`SiteClient` accepts a cookie; the server issues sessions at `POST /_/login`) — only the handshake is missing
- Gates `friendo push`, `friendo pull`, and the final push step of `friendo deploy`

### Client-side JS SDK (M4, not started)
- `friendo.js` built on TanStack Query
- RESTful API at `/_/api/*` for all content types (only bulk push/pull sync exists today — no per-record read/write)
- Declarative `data-friendo-*` attributes for progressive enhancement
- Comments, reactions, polls interactive out of the box

### Smaller loose ends
- **D1 migrations:** edge runtime should check schema version on first request and apply pending migrations (REFACTOR Open TODO)
- **Image `resize` filter:** stub in [renderer.go](runtime/go/renderer/renderer.go), needs a real implementation
