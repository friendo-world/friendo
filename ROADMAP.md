# Roadmap

What's shipped and what's next. For how the pieces fit together, see
[ARCHITECTURE.md](ARCHITECTURE.md).

| Phase | Scope | Status |
|---|---|---|
| **Phase 1** | CLI + Go runtime + friendo.world foundation | ✅ Complete |
| **Phase 1.5** | Auth, shared admin SPA, codebase refactor, Workers for Platforms, working deploy | ✅ Complete |
| **Phase 2** | Desktop editor (Tauri-based WYSIWYG) | Planned |
| **Phase 3** | Community features, template marketplace | 🔧 In progress |

---

## Phase 1 — Complete

The foundation: a Go binary that can init, serve, and export a site, plus a
Cloudflare Worker that serves deployed sites.

- `friendo init` / `serve` (hot reload, Pongo2, SQLite) / `export --mode static`
- `friendo deploy` — browser-based device auth, record sync, asset upload
- Password-protected admin UI; common schema identical in SQLite and D1
- Edge runtime with a custom Jinja2-compatible template engine (no `eval`)

**Key decisions:** common schema over arbitrary collections (sync is a copy, not a
migration); pure-Go SQLite (no CGO, single cross-platform binary); a custom JS
template engine for Workers (Nunjucks uses `eval`, blocked in Workers).

## Phase 1.5 — Complete

Hardened the foundation and made Friendo production-ready.

- **Auth** — `users` + `sessions` + `otp_codes` tables; email + password setup;
  full user management with a role hierarchy (superadmin > admin > editor >
  member); DB-stored sessions; legacy-admin migration path. Same model on the edge.
- **Codebase refactor** — `binary/` → `cli/` + `runtime/go/`; `world/` →
  `platform/` + `runtime/edge/`; single Go module at the repo root. Self-hosting
  became a first-class path running the exact code friendo.world runs.
- **Workers for Platforms** — `platform/worker.js` reshaped as a WfP dispatch
  Worker (subdomain routing, platform auth, provisioning); per-site D1 + R2 + user
  Worker; platform D1 simplified to a sites registry + Better Auth.
- **Shared admin SPA + REST API** — the admin UI is now one Preact SPA in `admin/`,
  built once and served byte-for-byte identically by both runtimes. Both implement
  the same runtime-agnostic REST API (`/_/api/*`): auth, records CRUD, users,
  settings, first-run setup/migrate, and sync. The old server-rendered Go templates
  and the edge's JS-string admin were removed — the SPA is the single source of
  truth. Every endpoint verified on the Go runtime and on the edge under miniflare.
- **CLI deploy/push/pull** — `push`/`pull` authenticate against a site's
  `/_/api/auth/login`, caching the session in `~/.friendo/config`; a fresh/empty
  target is bootstrapped via `/_/api/setup` (so deploy creates the first admin
  automatically). Verified end-to-end against a running runtime.
- **Edge D1 migrations** — applied once per isolate on first request, tracked in a
  `schema_migrations` table; a fresh D1 self-initializes.
- **Image `resize` filter** — implemented in both runtimes as a CDN resize hint.

## Phase 2 — Desktop editor (planned)

A Tauri-based WYSIWYG editor for authoring sites without hand-editing templates.
An independent track — it doesn't depend on the community work below. (No `editor/`
code exists in the repo yet.)

## Phase 3 — Community features (in progress)

Bringing the rest of the data model to life: comments, reactions, and polls
written by site **visitors**. Visitors become passwordless `member` accounts by
verifying their email (OTP). Full design + slices in
[docs/phase-3-community.md](docs/phase-3-community.md).

- **3a — Identity foundation** ✅ — a shared Go/edge migration mechanism; the
  accounts/profiles split (`users` = accounts, `authors` = profiles linked by
  `user_id`, one account → many profiles); a default profile per account;
  `author_id` → `authors.id`; OTP `request-code` / `verify-code`. Edge schema
  brought to full parity with Go. Verified by the parity harness on both runtimes.
- **3b** — comments + moderation queue.
- **3c** — reactions + polls.
- **3d** — `friendo.js` SDK (`data-friendo-*` progressive enhancement).
- **3e** — hardening: rate limiting, real email provider, moderation toggle,
  persona switcher.

## Testing

A **parity test harness** in [tests/](tests/) runs one shared `scenarios.json`
against **both** runtimes and asserts identical behavior — encoding the project's
"one UI, two runtimes, identical behavior" guarantee. It covers the core REST API:
auth, first-run setup, records CRUD, users + role enforcement, settings, the
session lifecycle, and OTP member login. Run with `npm test` (or `npm run test:go`
/ `test:edge`). It already earned its keep — it caught a 401-vs-403 divergence
between the runtimes during Phase 3a.

Still uncovered (worth growing as those areas land): the template/rendering layer,
the sync push/pull endpoints, and the CLI deploy flow.

## Known gaps / tech debt

- **friendo.world provisioning is unverified.** The platform Worker's Cloudflare-API
  provisioning (D1 + R2 + user Worker creation) needs a real Cloudflare account and
  `CF_API_TOKEN` to exercise end-to-end; it's never been run against live Cloudflare.
