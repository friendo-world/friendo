# Phase 1 — Complete

Phase 1 delivered the foundation: a Go binary that can init, serve, and export a site, plus a Cloudflare Worker that serves deployed sites via friendo.world.

**What shipped:**
- `friendo init` — scaffold a site directory
- `friendo serve` — local dev server with hot reload, Pongo2 templates, SQLite
- `friendo deploy` — browser-based device auth, record sync to D1, asset upload to R2
- `friendo export --mode static` — flat HTML export
- Admin UI at `/_/` — password-protected CRUD for all content types
- Edge runtime — Cloudflare Worker with custom Jinja2-compatible template engine (no eval)
- Common schema — identical table structure locally (SQLite) and on the edge (D1)

**Key decisions:**
- Common schema over arbitrary collections (enables fast data sync, not schema migration)
- Pure Go SQLite (`modernc.org/sqlite`) — no CGO, single cross-platform binary
- Custom JS template engine for Workers (Nunjucks uses eval, blocked in Workers)
- Better Auth for platform identity, simple bcrypt for local admin

See git history for the full original planning document.
