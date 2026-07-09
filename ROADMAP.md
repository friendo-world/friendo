# Roadmap

What's shipped and what's next. For how the pieces fit together, see
[ARCHITECTURE.md](ARCHITECTURE.md).

| Phase | Scope | Status |
|---|---|---|
| **Phase 1** | CLI + Go runtime + friendo.world foundation | ✅ Complete |
| **Phase 1.5** | Auth, shared admin SPA, codebase refactor, Workers for Platforms, working deploy | ✅ Complete |
| **Phase 2** | Desktop editor (Tauri-based WYSIWYG) | Planned |
| **Phase 3** | Community features (comments, reactions, polls, SDK) | ✅ Core complete |

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
  full user management with a role hierarchy (owner > admin > editor > contributor
  > member; see the auth redesign below); DB-stored sessions; legacy-admin
  migration path. Same model on the edge.
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

## Phase 3 — Community features (core complete)

Brings the rest of the data model to life: comments, reactions, and polls
written by site **visitors**. Visitors become passwordless `member` accounts by
verifying their email (OTP). Slices 3a–3e are shipped and parity-tested on both
runtimes (deferred items noted per slice below). Full design + slices in
[docs/content/docs/phase-3-community.md](docs/content/docs/phase-3-community.md)
(also published at `docs.friendo.world/docs/phase-3-community`).

- **3a — Identity foundation** ✅ — a shared Go/edge migration mechanism; the
  accounts/profiles split (`users` = accounts, `authors` = profiles linked by
  `user_id`, one account → many profiles); a default profile per account;
  `author_id` → `authors.id`; OTP `request-code` / `verify-code`. Edge schema
  brought to full parity with Go. Verified by the parity harness on both runtimes.
- **3b — Comments + moderation** ✅ — `GET/POST /_/api/posts/:id/comments`
  (member-gated write, public reads see approved only), a `comments.status`
  column (migration `0004`), admin moderation endpoints (`GET /comments`,
  `PUT/DELETE /comments/:id`) and a Comments queue in the admin SPA.
- **3c — Reactions + polls** ✅ — toggle `GET/POST /_/api/reactions`; poll
  `POST /_/api/polls` (admin), `GET /_/api/polls/:id`, `POST /_/api/polls/:id/vote`
  (member, one vote each, closed-poll guard); unique indexes (migration `0005`).
  Polls can also be **authored in a post's front matter** (`poll: {slug, question,
  options}`, migration `0007`): `GET /_/api/polls/by-slug/:slug` lazily creates the
  poll on first view and syncs its text on later edits while preserving votes. The
  record API accepts an optional `data` field so front-matter fields round-trip.
- **3d — `friendo.js` SDK** ✅ — dependency-free **Web Components**
  (`<friendo-auth>`, `<friendo-comments>`, `<friendo-reactions>`, `<friendo-poll>`)
  styleable via `::part()`. Built by `npm run sdk` and served byte-identically at
  `/friendo.js` by both runtimes. (Supersedes the earlier `data-friendo-*` sketch.)
- **3e — Hardening** ✅ (core) — comment auto-approve toggle (persisted
  `site_settings`, migration `0006`, `GET/PUT /_/api/settings` + SPA switch);
  a durable `request-code` rate limit (429); and an email-provider seam
  (Resend via `RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`; the OTP echo disables once
  configured). **Deferred:** the persona switcher UI and per-member comment-rate
  limiting.

Not yet wired: exposing approved comments/reactions/polls in the *server-side*
template context (`{{ post.comments }}`). The SDK delivers them client-side
today; server-side relations need a lazy-load mechanism in both template engines.

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

## friendo.world provisioning — validated

The platform Worker's Cloudflare-API provisioning (D1 + R2 + user Worker
creation) was **validated end-to-end against live Cloudflare** on the `dev`
dispatch namespace: `POST /api/sites` provisions the resources, and a request to
`{sub}.local.friendo.world` dispatches through to the per-site user Worker
serving from its own D1. `DELETE /api/sites/:id` tears all of it back down, and
partial-provision failures roll back their own resources. Re-running
`friendo deploy` (or `POST /api/sites/:id/redeploy`) re-pushes the current runtime
bundle to an existing site's Worker, so runtime fixes reach already-provisioned
sites. The runtime artifacts the provisioner deploys are built + published to R2
with `npm run runtime:bundle && npm run runtime:publish`.

## Production

The platform is **live at [friendo.world](https://friendo.world)** with full
multi-tenancy. The dispatch Worker runs in the `production` dispatch namespace
with its own prod D1 (`friendo-world`); the apex is a Workers Custom Domain (auto
DNS + TLS); the `*.friendo.world/*` route dispatches tenant sites; and a single
proxied **`*.friendo.world` wildcard DNS record** makes every subdomain resolve
instantly (Universal SSL covers it). The whole lifecycle — provision → live tenant
over HTTPS → destroy — is verified live.

**Provisioning is DNS-free and fast (~3s):** `POST /api/sites` creates just a D1
database, an R2 bucket, and the user Worker. No per-site DNS (the wildcard handles
it) and no schema pre-apply (the site's D1 self-initializes on first request).
This removed the old per-site-DNS record management and the propagation/negative-
cache wait entirely. (Runs on **Workers Paid** — WfP requires it, and it lifts
D1's per-account cap from 10 to 50,000.)

## Recently shipped

- **Auth & permissions redesign (toward 0.2)** — capability-based roles (owner >
  admin > editor > contributor > member) with an **ownership axis** (`edit.own` vs
  `edit.any`), so contributors edit only their own posts and authors moderate
  comments on their own posts. Renamed `superadmin` → `owner` with co-owners and a
  **last-owner guard** (migration `0008`); per-site access presets (Personal /
  Community / Blog) backed by `access.*` + `content.require_approval` settings; a
  post-approval workflow; and a published-only public render path. Both runtimes at
  parity (109 test steps). Design: [design/auth-permissions.md](design/auth-permissions.md).
- **File-based `content/` authoring** — Hugo-style markdown → DB, with a `data`
  JSON column for arbitrary front matter, `friendo build`, serve/push integration,
  and a `sort_by` filter. See [ARCHITECTURE.md](ARCHITECTURE.md).
- **`markdown` filter** in both runtimes (goldmark / marked), plus edge template
  engine fixes (`{# comments #}`, filters in `{% for %}` iterables).
- **Distribution** — GoReleaser cross-platform binaries + `curl | sh` installer +
  `go install`; `friendo --version`.
- **Site lifecycle CLI** — `friendo build` / `redeploy` / `destroy`.

## Known gaps / tech debt

- **No bulk runtime rollout.** Redeploy is per-site and owner-only; pushing a
  runtime update across *all* sites at once would need an operator role (the
  platform has no admin scope yet) or a script iterating owned sites.
- **Customer custom domains** (`friendo.toml deploy.domain` → a user's own
  domain, e.g. Cloudflare for SaaS) aren't wired up yet.
- **Image galleries / page bundles** (folder of images + `index.md`) are the
  planned phase 2 of file-based content — needs a binary-safe asset push.
