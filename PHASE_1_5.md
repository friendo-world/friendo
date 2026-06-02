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

### Codebase refactor (M5)
- `binary/` → `cli/` + `runtime/go/`
- `world/` → `platform/` + `runtime/edge/`
- Single Go module at repo root (`github.com/henryholtgeerts/friendo`)
- Sync API (`/_/api/*` push/pull endpoints) extracted into both runtimes
- `deploy` → interactive wizard with target selection (friendo.world, Cloudflare, VPS)
- `push` / `pull` as ongoing sync verbs with `--data` / `--users` flags
- Edge runtime (`runtime/edge/index.js`) extracted as standalone single-site Worker
- Self-hosting is now a first-class path — identical code to friendo.world

## Remaining

### Admin UI rebuild (M3)
- Move templates from Go string constants to `embed.FS` files
- Add settings page, richer dashboard, responsive layout

### Client-side JS SDK (M4)
- `friendo.js` built on TanStack Query
- RESTful API at `/_/api/*` for all content types
- Declarative `data-friendo-*` attributes for progressive enhancement
- Comments, reactions, polls interactive out of the box

### Workers for Platforms integration (M6)
- Reshape `platform/worker.js` as a WfP dispatch Worker
- Strip all site rendering / admin UI / sync code from the platform
- Platform becomes: subdomain routing, landing page, dashboard, platform auth, provisioning API
- Provisioning creates per-site D1 + R2 + deploys user Worker via Cloudflare API
- Platform D1 schema simplified to sites registry + Better Auth tables only
- Each site on friendo.world runs as an isolated user Worker (`runtime/edge/index.js`)
- Update CLI `friendo deploy` to use new provisioning API
- See [REFACTOR.md](./REFACTOR.md) for full architecture details
