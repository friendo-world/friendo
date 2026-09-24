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
| **Runtime** | The thing that actually runs a site: serves pages, stores data, hosts the admin UI and API. One implementation — the Go runtime. | `runtime/go/` |
| **Network mode** | friendo.world managed hosting: the same Go binary in `friendo network serve`, dispatching many sites by subdomain, with the network's own account + operator surface built from `<friendo-*>` tags. Optional — a single site never needs it. | `runtime/go/network/` |

A **site is a folder**: `friendo.toml`, `layouts/`, `pages/`, `assets/`, and a
`data/` database. It's portable, checkable into git, and outlives any single tool.

> friendo previously shipped a second runtime — a Cloudflare Worker ("edge") build
> — plus a Workers-for-Platforms hosting layer, kept in lockstep by a parity harness.
> Both were removed when the project unified on the Go runtime; the sections below
> describe the one runtime that remains.

## One runtime, one site

Every Friendo site — wherever it runs — exposes the same interface:

- `/` — the site (templates + data)
- `/_/` — the admin UI
- `/_/api/*` — the REST + sync API

One runtime implements it — the **Go runtime** (`runtime/go/`):

| | Go runtime (`runtime/go/`) |
|---|---|
| Runs as | A single Go binary (`friendo serve`), e.g. on a VPS |
| Data | SQLite (`modernc.org/sqlite`, no CGO) |
| Assets | `assets/` on disk (uploads land in `assets/uploads/`); media can offload to S3/R2 via `FRIENDO_S3_*` (`runtime/go/storage`) |
| Templates | [Pongo2](https://github.com/flosch/pongo2) (Jinja2) |
| Realtime | in-process message hub (SSE) |

The client transport is the same everywhere — `<friendo-channel>` opens one
`EventSource` on `/channels/:id/stream` and new messages stream in live.

**One schema.** The runtime defines its tables once (`posts`, `comments`,
`reactions`, `channels`, `messages`, `polls`, `poll_votes`, `authors`,
`locations`, `files`, `users`, `sessions`). One schema across every site is what
makes data sync a copy, not a migration.

**Same binary from laptop to production.** Local dev, self-host, and
friendo.world all run this one binary — `friendo serve` for a single site,
`friendo network serve` for many (see *Managed hosting* below). There's no
managed/self-hosted divergence because there's nothing else to run.

## The admin UI: one embedded SPA

The admin UI is a single Preact + TypeScript app in `admin/`, built once and
embedded into the runtime, so what ships is always what you built:

```
admin/  ──vite build──▶  runtime/go/admin/spa/   (embedded via go:embed)
```

The SPA is pure client-side; it talks only to the REST API, so one build serves
every site.

## REST + sync API (`/_/api/*`)

The runtime implements these endpoints. Most require site admin auth (a
`friendo_session` cookie); the bootstrap endpoints are public so the SPA and CLI
can start cold.

Access is **capability-based**, not a fixed admin gate (see *Auth model* below):
each route requires a capability, and content/comment routes additionally scope to
the actor's *own* rows unless they hold the `.any` variant.

| Area | Endpoints | Capability |
|---|---|---|
| Bootstrap | `GET /me`, `POST /auth/request-code`, `POST /auth/verify-code` (the default sign-in, any role), `POST /auth/login` (password — only when `access.password_login` is on), `POST /auth/logout`, `GET /setup`, `POST /setup/request-code`, `POST /setup`, `POST /migrate` (gated on the legacy password) | public |
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
no-JS read, the runtime *also* attaches the public relations to the focused record
in the template context — `record.comments` (approved), `record.reactions`,
`record.poll`, and `record.gallery` — so a post template can render them directly.
Inherently interactive features (realtime chat, the `<friendo-map>` widget) stay
client-side by design.

## Deploy, push, pull

The CLI talks to a site's own `/_/api/*` — whatever the host (friendo.world, your
own network, or a single self-hosted site).

- **`friendo deploy [subdomain] [--network URL]`** — publish the current folder to
  a network (friendo.world by default). It signs you in in the browser (device-auth)
  if needed, claims a subdomain your account owns, exchanges an in-process SSO
  session for the site, and pushes. See *Managed hosting* below.
- **`friendo push`** — upload templates + assets (`--data` / `--users` to include
  records and accounts). Assets are base64-encoded so binary files (uploaded
  images in `assets/uploads/`) round-trip intact, and `--data` also carries
  `files` rows so a deployed site's media metadata matches the source.
- **`friendo pull`** — fetch remote records/accounts (and media rows) back into
  the local database.

**Auth + first-run bootstrap.** `push`/`pull` authenticate via `authenticateSite()`:
reuse a cached session from `~/.friendo/config`, else log in. On a network,
`friendo deploy` seeds that session from the network's SSO exchange (your account
becomes the site owner). For a self-hosted site with **no admin yet**,
`GET /_/api/setup` reports it and the CLI walks the user through creating the first
admin via `POST /_/api/setup`.

## File-based content (`content/`)

Content normally lives in the DB (edited via the admin UI or API). A site can
*also* be authored as **files** — a Hugo-style `content/` folder that compiles
into that same DB, so the runtime is untouched: `content/` is just another way to
fill the same tables.

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

## Managed hosting: network mode

friendo.world is just the reference **friendo network** — the same Go binary in
`friendo network serve`, dispatching many sites by subdomain in one process. Any
self-hoster can run their own network the same way; there's no separate platform
codebase.

```
testsite.friendo.world
  → one friendo binary (friendo network serve)
    → dispatcher reads the Host header, resolves "testsite"
  → BuildSiteHandler(testsite)   — serves the site from its folder + SQLite DB
```

| Component | Role |
|---|---|
| **Dispatcher** (`runtime/go/network`) | Reads `r.Host`, resolves the site, serves its cached handler from `BuildSiteHandler`. Opens each site's DB on demand; LRU-caches handlers and closes idle ones. Per-request panic recovery isolates one tenant's failure from the rest. |
| **Registry** | A small SQLite DB mapping `subdomain → site folder + display name + owner account`. |
| **Apex** | The bare domain serves the operator's chosen **home site** (a normal tenant site), with the network's own surface layered over it: `/api/*` always, and four **default pages** — `/account` + `/login` (`<friendo-account>`), `/network` (`<friendo-console>`), `/activate` (`<friendo-activate>`) — unless the home site defines that page itself. `www.` redirects to the apex. |
| **Account + operator API** | `/api/account/*` (your sites, domains, the one-time "Open admin" link) and `/api/network/*` (every operator lever), reachable by the account cookie (browser) or a Bearer token (CLI). |
| **Accounts store** | Network accounts + sessions + device-auth + OTP; `operator` is a capability; network settings (signup policy, site limit, home site). |

**One process, many tenants.** A tenant is "a folder + a SQLite file opened on
demand," so an idle site costs about a file handle and cold start is near-zero.
Because it's one binary, a hot or abusive tenant can be moved to its own container
running the same binary — density *or* isolation, chosen per site.

**Substrate.** friendo.world runs this binary on **Coolify + Hetzner** with a
persistent volume for the site folders + SQLite DBs. **Cloudflare** is dumb infra:
one proxied `*.friendo.world` wildcard DNS record (so every subdomain resolves
instantly) plus TLS/CDN, with a Cloudflare Origin cert on the box (SSL Full-strict).
**Cloudflare R2** holds media (`FRIENDO_S3_*`; see the runtime's storage layer
above). Full runbook in [DEPLOY.md](DEPLOY.md).

**Provisioning is a folder op.** Creating a site scaffolds its folder, opens its DB
(auto-migrates), records its owner account, and creates the owner user inside it
(a code-only account) — whether a tenant claimed it or an operator provisioned it,
so no site is ever left with its first-run setup open. Teardown reverses it. There
is no per-site Worker, no per-site DNS, and no runtime bundle to publish — deploying
the network updates every site at once (bulk rollout is automatic).

**Into a site's admin.** Cookies are per host, so the network can't sign a browser
into `sub.<base>` directly. `POST /api/account/sites/{sub}/admin-link` mints the site
session (as the CLI's `/api/sso/exchange` does) and parks it behind a one-time code;
the browser lands on `https://sub.<base>/_/sso?code=…`, which the dispatcher redeems
into a `friendo_session` cookie before the site ever sees the request. Owners only —
operators are not exempt.

**Accounts & deploy.** `friendo login` runs a browser device-auth against the
network (approved on `/activate`) and caches an account token. `friendo deploy
[subdomain]` claims a subdomain your account owns (reserved names refused), exchanges
an in-process SSO session for the site, and pushes. Operators (accounts with the
`operator` capability) manage the fleet via `friendo network *` — on the box or over
`/api/network/*` from anywhere — and the `/network` page; the first sign-in claims
operator when none exists, and `FRIENDO_OPERATOR_EMAIL` designates the bootstrap
operator. Full design in [design/network-accounts.md](design/network-accounts.md)
and [design/v0.5-roadmap.md](design/v0.5-roadmap.md).

## Schema migrations

The runtime uses an ordered migration list whose baseline (id 1) is the schema
itself, tracked in a `schema_migrations` table. Migrations apply in `data.Open`; a
freshly provisioned database self-initializes from the baseline. Add a migration by
dropping a new `NNNN_*.sql` file in `runtime/go/data/migrations/` — one schema, one
place.

## Auth model

| Scope | What | Where |
|---|---|---|
| **Site** | Per-site **accounts** (`users`) with roles (owner > admin > editor > contributor > member). Sign-in is an emailed code for every role; a bcrypt password is optional and only usable when `access.password_login` is on. DB-stored sessions. On localhost with no email provider, `friendo serve` skips sign-in for same-machine requests. | site SQLite |
| **Network** | **Network accounts** (`runtime/go/network`) for signing in to a friendo network and owning sites — the same email-code sign-in (cookie in a browser, Bearer token from the CLI via device-auth). `operator` is a capability (no operator password), separate from per-site roles. | network accounts DB |

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
[design/phase-3-community.md](design/phase-3-community.md).

Bcrypt hashes are portable, so `friendo push --users` carries accounts to a
deployed site unchanged (a blank incoming hash never overwrites a real one).
