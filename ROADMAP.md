# Roadmap

What's shipped and what's next. For how the pieces fit together, see
[ARCHITECTURE.md](ARCHITECTURE.md).

| Phase | Scope | Status |
|---|---|---|
| **Phase 1** | CLI + Go runtime + friendo.world foundation | ✅ Complete |
| **Phase 1.5** | Auth, shared admin SPA, codebase refactor, multi-tenant deploy | ✅ Complete |
| **Single-runtime pivot** | Unify on the Go runtime; friendo.world → network mode (Coolify + Hetzner) | ✅ Shipped |
| **Phase 2** | Desktop editor (Tauri-based WYSIWYG) | Planned |
| **Phase 3** | Community features (comments, reactions, polls, SDK) | ✅ Core complete |

---

## Single-runtime pivot — shipped (2026-07)

friendo unified on **one runtime, the Go runtime**. friendo.world now runs **network mode** —
one Go binary (`friendo network serve`) dispatching many sites by subdomain — on **Coolify +
Hetzner**, with **Cloudflare** as dumb infra (wildcard DNS + TLS/CDN) and **Cloudflare R2** for
media. The old "two runtimes, identical behavior, parity harness" framing is retired. Full
hosting guide: [DEPLOY.md](DEPLOY.md); accounts/auth design:
[design/network-accounts.md](design/network-accounts.md).

- **Consolidation** — unify on the Go runtime; the Cloudflare Worker "edge" runtime
  (`runtime/edge/`) and Workers-for-Platforms code (`platform/`) have been **deleted**,
  along with the dual-runtime parity harness (the Go `tests/` fixtures remain as a
  single-runtime regression suite).
- **Network mode** — a multi-tenant Go dispatcher + sites registry + provisioner
  (`friendo network serve`), per-request panic isolation, an LRU handler/DB cache, and an
  operator console/API.
- **S3/R2 media** — `runtime/go/storage` (`FRIENDO_S3_*`) offloads `assets/uploads/*` +
  `assets/galleries/*`; disk is the default.
- **Network accounts** — one account system with `operator` as a capability; passwordless
  email-OTP + device-auth (`friendo login` / `whoami` / `logout`); `FRIENDO_OPERATOR_EMAIL`
  bootstraps the first operator, and the first console sign-in claims operator when none exists.
- **Self-service deploy** — `friendo deploy [subdomain] [--network URL]` (device-auth →
  claim/own the subdomain → in-process SSO session → push); a signup policy (open/invite,
  default invite) with invites and an operator console UI.
- **Retirements** — the operator-password path and `friendo network login` are gone; the WfP
  platform-client is removed from the CLI (top-level `redeploy`/`destroy` and `operator add`
  gone → `friendo network destroy` / `friendo network operator grant`).
- **friendo.world cutover** — live on Coolify + Hetzner + Cloudflare R2.

**Next:** polish — quotas, richer `network accounts` / `signups` management, and custom
domains.

---

## Phase 1 — Complete

The foundation: a Go binary that can init, serve, and export a site, plus a
Cloudflare Worker that serves deployed sites. *(The edge Worker has since been removed
— see the single-runtime pivot above.)*

- `friendo init` / `serve` (hot reload, Pongo2, SQLite) / `export --mode static`
- `friendo deploy` — browser-based device auth, record sync, asset upload
- Password-protected admin UI; common schema identical in SQLite and D1
- Edge runtime with a custom Jinja2-compatible template engine (no `eval`) *(since removed)*

**Key decisions:** common schema over arbitrary collections (sync is a copy, not a
migration); pure-Go SQLite (no CGO, single cross-platform binary); a custom JS
template engine for Workers (Nunjucks uses `eval`, blocked in Workers).

## Phase 1.5 — Complete

Hardened the foundation and made Friendo production-ready.

- **Auth** — `users` + `sessions` + `otp_codes` tables; email + password setup;
  full user management with a role hierarchy (owner > admin > editor > contributor
  > member; see the auth redesign below); DB-stored sessions; legacy-admin
  migration path.
- **Codebase refactor** — `binary/` → `cli/` + `runtime/go/`; `world/` →
  `platform/` + `runtime/edge/`; single Go module at the repo root. Self-hosting
  became a first-class path running the exact code friendo.world runs.
- **Workers for Platforms (validated, since retired)** — `platform/worker.js` reshaped
  as a WfP dispatch Worker (subdomain routing, platform auth, provisioning); per-site
  D1 + R2 + user Worker; platform D1 simplified to a sites registry + Better Auth. This
  model was validated end-to-end on live Cloudflare, then **superseded by network mode**
  in the single-runtime pivot above; the WfP code has since been removed.
- **Shared admin SPA + REST API** — the admin UI is one Preact SPA in `admin/`, built
  once and served by the runtime over a REST API (`/_/api/*`): auth, records CRUD, users,
  settings, first-run setup/migrate, and sync. The old server-rendered Go templates were
  removed — the SPA is the single source of truth.
- **CLI deploy/push/pull** — `push`/`pull` authenticate against a site's
  `/_/api/auth/login`, caching the session in `~/.friendo/config`; a fresh/empty
  target is bootstrapped via `/_/api/setup` (so deploy creates the first admin
  automatically). Verified end-to-end against a running runtime. (Everyday deploy now
  goes through network device-auth — see the pivot above.)
- **Image `resize` filter** — a CDN resize hint.

## Phase 2 — Desktop editor (planned)

A Tauri-based WYSIWYG editor for authoring sites without hand-editing templates.
An independent track — it doesn't depend on the community work below. (No `editor/`
code exists in the repo yet.)

## Phase 3 — Community features (core complete)

Brings the rest of the data model to life: comments, reactions, and polls
written by site **visitors**. Visitors become passwordless `member` accounts by
verifying their email (OTP). Slices 3a–3e are shipped (deferred items noted per
slice below). Full design + slices in
[docs/content/docs/phase-3-community.md](docs/content/docs/phase-3-community.md)
(also published at `docs.friendo.world/docs/phase-3-community`).

- **3a — Identity foundation** ✅ — a shared migration mechanism; the
  accounts/profiles split (`users` = accounts, `authors` = profiles linked by
  `user_id`, one account → many profiles); a default profile per account;
  `author_id` → `authors.id`; OTP `request-code` / `verify-code`.
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
  styleable via `::part()`. Built by `npm run sdk` and served at `/friendo.js` by the
  runtime. (Supersedes the earlier `data-friendo-*` sketch.)
- **3e — Hardening** ✅ (core) — comment auto-approve toggle (persisted
  `site_settings`, migration `0006`, `GET/PUT /_/api/settings` + SPA switch);
  a durable `request-code` rate limit (429); and an email-provider seam
  (Resend via `RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`; the OTP echo disables once
  configured). Per-member comment/message rate limiting and the persona switcher
  (once deferred) both shipped in v0.3.

Server-side relations are now wired (v0.3, Tier 0): a post template can render
`{{ record.comments }}`, `{{ record.reactions }}`, and `{{ record.poll }}` directly
— approved/public data attached to the focused record, no JS required. The SDK
components remain the interactive, signed-in path.

**Shipped (added v0.3 scope):** a `<friendo-form>` submission component — ordinary
inputs plus rich `<friendo-input>` types (rich text, location, media, tags) that
create a post with arbitrary `data` metadata from the frontend, reusing the existing
records-create endpoint. Contributors author directly; members submit into the review
queue behind the opt-in `content.accept_submissions` setting (with a submission rate
limit). Design: [design/v0.3-friendo-form-plan.md](design/v0.3-friendo-form-plan.md).

## Testing

The **Go test suite** in [tests/](tests/) is the primary check. Its shared
`scenarios.json` (`npm run test:go`) covers the core REST API: auth, first-run setup,
records CRUD, users + role enforcement, settings, the session lifecycle, and OTP
member login.

The suite began as a **parity harness** that ran the same scenarios against both
runtimes to enforce the old "one UI, two runtimes, identical behavior" guarantee — it
earned its keep, catching a 401-vs-403 divergence during Phase 3a. With the
single-runtime pivot the edge runner is gone; the same fixtures now stand alone as the
Go regression suite (`go test ./...`).

Still uncovered (worth growing as those areas land): the template/rendering layer,
the sync push/pull endpoints, and the CLI deploy flow.

## friendo.world — live on network mode

friendo.world is **live** and runs the **network-mode** Go binary (`friendo network
serve`) on **Coolify + Hetzner**, with **Cloudflare** as dumb infra in front and
**Cloudflare R2** for media. Cloudflare's role is a proxied **`*.friendo.world` wildcard
DNS record** (every subdomain resolves instantly) plus wildcard TLS/CDN — no Workers, no
dispatch namespaces, no per-account D1 cap. Full walkthrough: [DEPLOY.md](DEPLOY.md).

- **One binary, many sites.** A single Go process dispatches tenant sites by subdomain
  from a sites registry; the apex serves the operator console. Provisioning a site
  (`friendo network provision`, the console, or self-service `friendo deploy`) creates
  its site folder + SQLite DB on the mounted `/data` volume — DNS-free (the wildcard
  handles it) and fast.
- **Media on R2.** `FRIENDO_S3_*` points the storage backend at a Cloudflare R2 bucket so
  the box's disk holds only SQLite DBs + site folders; disk is the fallback.
- **Passwordless everywhere.** Operator console, `friendo login`, and member login all
  deliver an email OTP (Resend via `RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`).

The old Workers-for-Platforms stack it replaced (dispatch namespaces, prod D1
`friendo-world`, per-site user Workers) has been **removed** from the repo.

## Recently shipped

- **v0.3 — consolidation toward 1.0** (Tiers 0–3; see
  [design/v0.3-roadmap.md](design/v0.3-roadmap.md)). Makes the shipped surface solid
  end-to-end rather than adding a new marquee. **Tier 0 — server-side community
  content:** a post template renders `{{ record.comments }}` / `.reactions` /
  `.poll` directly (approved/public data, lazy-loaded) so community
  data reads with no JS; the SDK stays the interactive path. **Tier 1 — galleries:**
  a page bundle (`content/blog/trip/index.md` + sibling images) auto-attaches its
  images as `field="gallery"` files (deterministic ids → idempotent re-import + push)
  rendered via `{{ record.gallery }}`. **Tier 2 — coverage past REST:** a shared
  render suite (`render-scenarios.json`), a hardened + tested
  SQL statement splitter (`splitter-cases.json`), a CLI push/pull round-trip test,
  and a Playwright SDK browser check (`npm run test:sdk`). **Tier 3 — polish:** a
  `<friendo-map>` SDK component (interactive Leaflet + OSM tiles, lazy-loaded), a
  **persona switcher** (`users.default_author_id`, migration `0010`; member
  endpoints `/_/api/me/personas`; a switcher in `<friendo-auth>`), and per-member
  comment/message rate limiting. The Go test suite
  now covers **162 steps + 5 render fixtures + 5 splitter cases**, plus the browser check.
- **v0.2 release hardening (Tier 0–2)** — the sequenced audit work toward the 0.2
  tag. **Security:** OTP echo gated behind an explicit dev flag + per-site email in
  provisioning; login/comment rate limiting. **Correctness:** `authors` +
  `site_settings` now sync; static export is published-only; the bundle scrubs
  `data/`. **Rendering:** template-engine hardening (nesting, `forloop.*`, filters,
  `'` escaping) with a CI render smoke; CORS removed + `Secure` cookies; a
  bundle-currency check on every PR. **Coherence:** provisioning idempotency; the
  once-dead `channels` / `messages` / `locations` / `files` tables are now wired up —
  **channels are a realtime feed** (SSE via a Go in-process hub), **locations** are
  geo-tags, and **media upload** attaches per-record images (multipart → `assets/` on
  disk / R2) that **sync on deploy** — a binary-safe (base64) asset push plus
  `files`-row sync so uploaded images travel byte-identical. The test suite now covers
  155 steps + render smoke; media upload + binary round-trip covered end-to-end.
  Roadmap + status: [design/v0.2-roadmap.md](design/v0.2-roadmap.md).
- **Auth & permissions redesign (toward 0.2)** — capability-based roles (owner >
  admin > editor > contributor > member) with an **ownership axis** (`edit.own` vs
  `edit.any`), so contributors edit only their own posts and authors moderate
  comments on their own posts. Renamed `superadmin` → `owner` with co-owners and a
  **last-owner guard** (migration `0008`); per-site access presets (Personal /
  Community / Blog) backed by `access.*` + `content.require_approval` settings; a
  post-approval workflow; and a published-only public render path. Design:
  [design/auth-permissions.md](design/auth-permissions.md).
- **File-based `content/` authoring** — Hugo-style markdown → DB, with a `data`
  JSON column for arbitrary front matter, `friendo build`, serve/push integration,
  and a `sort_by` filter. See [ARCHITECTURE.md](ARCHITECTURE.md).
- **`markdown` filter** (goldmark), plus template-engine fixes (`{# comments #}`,
  filters in `{% for %}` iterables).
- **Distribution** — GoReleaser cross-platform binaries + `curl | sh` installer +
  `go install`; `friendo --version`.
- **Site lifecycle CLI** — `friendo build`; site teardown is now `friendo network
  destroy` (the old top-level `redeploy`/`destroy` were removed in the pivot).

## Known gaps / tech debt

- ~~**No bulk runtime rollout.**~~ **Resolved by network mode:** one Go binary hosts
  every site, so a runtime update rolls out to all tenants when the single container
  redeploys; the operator console/API provides the admin scope.
- **Customer custom domains** — pointing a tenant's own domain at their site (beyond
  `*.friendo.world` subdomains) isn't wired up yet; a future item under network mode.
- ~~Image galleries / page bundles~~ — **shipped in v0.3 (Tier 1):** a page bundle's
  sibling images auto-import as `field="gallery"` files and render via
  `{{ record.gallery }}` (and a `<friendo-gallery>` remains an optional client-side
  nicety, not built since the server-side path covers it).
