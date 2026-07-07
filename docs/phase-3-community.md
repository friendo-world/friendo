# Phase 3 — Community

Activating the visitor-facing half of the data model: comments, reactions, and
polls, written by verified visitors. See [ARCHITECTURE.md](../ARCHITECTURE.md) for
the base design this builds on.

## Identity model: accounts vs. profiles

- **`users` = accounts** — the auth identity: email, optional password, OTP, a
  permission `role` (superadmin > admin > editor > member), unique per `(site, email)`.
- **`authors` = profiles** — display personas attached to an account via
  `authors.user_id`. **One account → many profiles.** All content (`posts`,
  `comments`, `reactions`, `poll_votes`) references a profile through `author_id`
  → `authors.id`.

Every account gets a **default profile** on creation (admins at setup, members at
OTP verification). Multiple profiles are supported structurally; the UI to
create/switch personas is deferred. Permissions live on `users.role`;
`authors.role` is unused.

## Visitors are OTP-verified members

A visitor becomes a `member` account by verifying their email — no password.

- `POST /_/api/auth/request-code` `{email}` — find-or-create the account (role
  `member`, `auth_methods=["otp"]`) and its default profile, store a hashed 6-digit
  code in `otp_codes` (~10 min, single-use), and send it. Rate-limited.
- `POST /_/api/auth/verify-code` `{email, code}` — validate → create a session →
  set the `friendo_session` cookie. `/me` then returns the member.

Members reuse the existing `sessions` cookie and role gate: `requireSiteAdmin`
(admin+) already excludes them, so a member authenticates but can't reach admin
APIs. Member-facing endpoints just require *any* authenticated user.

**Dev/test:** with no email provider configured, `request-code` returns the code
in its response (clearly gated) so local dev and the parity tests work. Wiring a
real provider (slice 3e) disables the echo.

## Public write API (slices 3b–3c)

Reads are open; writes require a member session.

| Endpoint | Notes |
|---|---|
| `GET/POST /_/api/posts/:id/comments` | post = member; default status **pending** (moderation) |
| `GET/POST /_/api/reactions` | toggle; unique `(site, target_type, target_id, author_id, emoji)` |
| `GET /_/api/polls/:id` · `POST /_/api/polls/:id/vote` | one vote per `(poll, author_id)` |

Moderation is admin-gated: `GET /_/api/comments?status=pending`, `PUT/DELETE
/_/api/comments/:id`, surfaced as a queue in the admin SPA. Default is
pre-moderation; an auto-approve toggle comes in 3e.

## Schema evolution

Both runtimes now share a migration mechanism (a `schema_migrations` table; the
baseline schema is migration 1). The Go runtime applies migrations in `data.Open`;
the edge applies them once per isolate on first request. Changes land as new
`migrations/NNNN_*.sql` files in both runtime trees.

- **0002 — author profiles:** add `authors.user_id` + index; backfill a default
  profile per existing account; remap existing `posts.author_id` from account id →
  profile id.
- **later:** `comments.status`; unique indexes on `reactions` and `poll_votes`.

## `friendo.js` SDK (slice 3d)

A small client built and served by both runtimes (the admin-bundle pattern),
driven by progressive-enhancement attributes — `data-friendo-comments`,
`data-friendo-react`, `data-friendo-poll` — so a site author drops in a tag and
gets interactive, member-gated UI with no custom JS.

## Slices

- **3a — Identity foundation** *(in progress)*: shared migration mechanism;
  accounts/profiles split (0002); default profile per account; `author_id` →
  `authors.id`; OTP request/verify. Parity tests.
- **3b — Comments + moderation.**
- **3c — Reactions + polls.**
- **3d — `friendo.js` SDK.**
- **3e — Hardening:** rate limiting, real email provider, moderation toggle, an
  author/persona switcher.

Each slice extends `tests/scenarios.json`, keeping Go/edge parity enforced.
