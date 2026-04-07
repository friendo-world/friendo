# Friendo — Phase 1 implementation plan

Phase 1 delivers the two things that make Friendo real: a binary you can build a site with locally, and a deploy command that puts it on the internet. Everything else — the editor, the community features — builds on this foundation.

**Scope:** Friendo binary + CLI + Friendo.world (Cloudflare edge runtime + deploy pipeline)  
**Output:** A developer can `friendo init`, build a site, and `friendo deploy` it to a live URL.

---

## Tech stack

| Layer | Technology | Role |
|---|---|---|
| Runtime language | Go | Binary compilation, HTTP server, CLI |
| Database | PocketBase (embedded) | Local SQLite database + admin UI |
| Template engine | Pongo2 | SSR template rendering (Jinja2-compatible) |
| Edge runtime | Cloudflare Workers | Serves templates in production |
| Edge database | Cloudflare D1 | SQLite-compatible database at the edge |
| Asset storage | Cloudflare R2 | Media and static file hosting |
| Edge renderer | Nunjucks | Jinja2-compatible JS renderer for Workers |

---

## Project structure

```
friendo/
└── binary/
    ├── cmd/
    │   └── friendo/
    │       └── main.go          # CLI entrypoint
    ├── internal/
    │   ├── server/              # HTTP server and routing
    │   ├── renderer/            # Pongo2 rendering + filter registry
    │   ├── data/                # PocketBase integration
    │   ├── deploy/              # Deploy pipeline (schema export, Worker bundle)
    │   └── scaffold/            # friendo init logic
    ├── edge/
    │   ├── worker.js            # Cloudflare Worker (Nunjucks SSR)
    │   └── schema-migrate.js    # D1 migration runner
    └── friendo.toml.tmpl        # Site config template
```

---

## Milestones

### M1 — Bare binary

**Goal:** `friendo serve` renders a Pongo2 template from a local directory. No database, no CLI beyond serve. Just proof that the rendering layer works.

**Deliverables:**
- Go HTTP server reads `.html` files from a `/pages` directory
- Pongo2 renders templates with a basic context object (`{{ site.name }}`, `{{ request.path }}`)
- Template inheritance working (`{% extends %}`, `{% block %}`)
- `friendo serve` command starts the server on `localhost:3000`
- `--port` flag supported

**Done when:** A developer can create a `pages/index.html` with Pongo2 syntax, run `friendo serve`, and see it rendered in a browser.

---

### M2 — Data layer

**Goal:** PocketBase embedded. Collections queryable from templates. Dynamic routing working.

**Deliverables:**
- PocketBase embedded as a Go library (not a subprocess)
- SQLite database initialised in `/data/friendo.db` on first run
- Collections available in template context as `{{ collections.posts }}`, `{{ collections.pages }}`, etc.
- File-based dynamic routing: `pages/blog/[slug].html` resolves `/blog/my-post` against a PocketBase collection
- PocketBase admin UI accessible at `localhost:3000/_/` during `friendo serve`
- Custom Pongo2 filters registered: `asset_url`, `date`, `resize`

**Done when:** A developer can define a `posts` collection in PocketBase, create a few records, and render them in a template via `{% for post in collections.posts %}`.

---

### M3 — Full CLI

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
- Graceful handling of missing collections (warn, don't crash)

`friendo export`
- Mode 1 (static): renders all pages to HTML files in `/dist` — no server required to host
- Mode 2 (bundle): packages site directory + binary into a self-contained archive

`friendo deploy` *(stub only at this milestone — wired up in M5)*
- Authenticates with Friendo.world (OAuth flow, token stored in `~/.friendo/config`)
- Prints a clear error if the edge runtime isn't ready yet

**Done when:** A developer can go from zero to a running local site in under two minutes using only the CLI.

---

### M4 — Edge runtime

**Goal:** The Cloudflare Worker + D1 + R2 stack works. A site can be manually deployed and served from the edge with behavior matching local exactly.

**Deliverables:**

Worker (`edge/worker.js`):
- Nunjucks initialised with the same template directory structure as the binary
- Routing logic mirrors file-based routing from the binary
- D1 queried for collection data, injected into Nunjucks context identically to local PocketBase context
- R2 assets served via Worker or direct R2 public URL
- `asset_url` filter resolves to R2 URL in cloud context automatically

Schema migration (`edge/schema-migrate.js`):
- Reads PocketBase SQLite schema from `/data/friendo.db`
- Generates D1-compatible SQL migration
- Handles PocketBase's internal table conventions cleanly

Manual deploy verified end-to-end:
- Schema exported and applied to a D1 database
- Records migrated
- Assets uploaded to R2
- Worker deployed via Wrangler CLI
- Site live and correct at a `*.workers.dev` URL

**Done when:** A developer can follow a manual checklist and have a working cloud version of their local site.

---

### M5 — `friendo deploy`

**Goal:** The full deploy pipeline is automated behind a single command.

**Deliverables:**

`friendo deploy` pipeline (in order):
1. Authenticate with Friendo.world API (reuse token from M3 stub)
2. Export PocketBase schema → generate D1 migration SQL
3. Apply D1 migration via Friendo.world API
4. Diff and sync uploaded assets to R2 (only changed files)
5. Bundle Worker script with site templates
6. Upload Worker bundle via Cloudflare API
7. Assign `[site-name].friendo.world` subdomain (DNS via Cloudflare API)
8. Print live URL on success

Additional:
- Re-deploy is idempotent — running `friendo deploy` twice is safe
- `--dry-run` flag prints what would be deployed without doing it
- Custom domain support via `friendo.toml`:
  ```toml
  [deploy]
  domain = "mysite.com"
  ```
- Meaningful error messages at each step (auth failure, schema conflict, quota exceeded)

**Done when:** `friendo deploy` takes a fresh site from local to live in a single command with no manual steps.

---

## Friendo.world backend (parallel track)

The deploy pipeline requires a thin Friendo.world API. This runs in parallel with M4/M5.

**Endpoints needed:**

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/auth/token` | Issue deploy token (OAuth) |
| `POST` | `/api/sites` | Register a new site, reserve subdomain |
| `POST` | `/api/sites/:id/schema` | Accept and apply D1 migration |
| `POST` | `/api/sites/:id/assets` | Accept asset uploads, sync to R2 |
| `POST` | `/api/sites/:id/worker` | Accept Worker bundle, deploy to Cloudflare |
| `GET` | `/api/sites/:id` | Site status and deploy history |

The backend itself is a Friendo site — dogfooding from day one.

---

## Key technical decisions

**PocketBase as a library, not a subprocess**  
PocketBase can be embedded directly in a Go binary. This keeps the single-binary promise intact and avoids the complexity of managing a child process. The tradeoff is tighter coupling to PocketBase's Go API, which is acceptable at this stage.

**Nunjucks over WASM for the edge renderer**  
Running the Go binary as a WASM module inside a Cloudflare Worker is technically possible but constrained by the 1MB script size limit. Nunjucks is a mature Jinja2-compatible renderer that runs natively in Workers. Template syntax is compatible with Pongo2, so templates work identically in both environments without modification.

**Static export as a first-class feature**  
`friendo export --static` is not an afterthought. It's the escape hatch that makes Friendo's portability promise credible. Some sites have no dynamic content and should deploy to Cloudflare Pages or Netlify as a plain HTML folder. Friendo supports this from day one.

**SQLite → D1 migration strategy**  
PocketBase uses standard SQLite with some internal tables (`_collections`, `_admins`, etc.). The migration script exports only user-defined collection tables and their records. PocketBase internals are not migrated — the Worker has its own lightweight collection registry derived from `friendo.toml`.

---

## Open questions

- **Auth model for Friendo.world.** OAuth with GitHub login is the obvious choice for a developer-forward v1. Worth deciding before M3.
- **Rate limits and quotas.** What are the free tier limits on Friendo.world? (Worker requests, D1 reads, R2 storage.) Needs a decision before public launch.
- **Pongo2 filter parity.** The `resize` filter implies server-side image processing. In the cloud context this means a Worker that proxies R2 assets through image transformation (Cloudflare Images, or a simple sharp-based resize Worker). Scope for Phase 1 or defer to Phase 2?
- **Collection schema changes after deploy.** If a developer adds a field to a collection locally and re-deploys, the migration needs to be additive. Destructive migrations (dropping columns) should require an explicit flag. Define the policy before M5.

---

## Definition of done for Phase 1

Phase 1 is complete when a developer with no prior Friendo experience can:

1. Install the binary
2. Run `friendo init my-site`
3. Run `friendo serve` and see their site at `localhost:3000`
4. Edit a template and see the change reflected without restarting
5. Add a collection record in PocketBase and see it appear in the rendered page
6. Run `friendo deploy` and receive a live URL at `[name].friendo.world`

That loop — init, build, deploy — is the heartbeat of the project. Everything in Phase 1 serves it.
