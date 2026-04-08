# Development

## Prerequisites

- [Go 1.21+](https://go.dev/dl/)
- [Node.js](https://nodejs.org/)
- [Wrangler CLI](https://developers.cloudflare.com/workers/wrangler/install-and-update/) (`npm i -g wrangler`)
- `sqlite3` (for syncing local data to the edge)

## Project structure

```
friendo/
├── binary/          # Go source code (CLI, server, data, admin, renderer)
├── world/           # Cloudflare Worker + sync tooling (friendo.world)
├── testsite/        # Local sandbox for testing
└── package.json     # Dev scripts
```

## Quick start (three terminals)

**Terminal 1 — Local binary**
```bash
npm run serve:open
```
- `http://localhost:3000` — your site
- `http://localhost:3000/_/` — admin UI (no password with `--open-admin`)

**Terminal 2 — Edge Worker**
```bash
npm run world:install   # first time only
npm run world:sync && npm run world:dev
```
- `http://testsite.local.friendo.world:8787` — your site on the edge
- `http://local.friendo.world:8787` — landing page
- `http://local.friendo.world:8787/login` — sign in
- `http://local.friendo.world:8787/dashboard` — your sites

**Terminal 3 — Deploy via CLI**
```bash
npm run deploy:local
```
First run opens your browser to sign in. Token is saved for subsequent deploys.

## Local dev URLs

Local development uses `*.local.friendo.world` for subdomain routing, backed by a wildcard DNS record pointing to `127.0.0.1`. This means real subdomain routing works locally — no `?site=` query params needed.

| URL | What it shows |
|---|---|
| `http://local.friendo.world:8787` | Landing page |
| `http://local.friendo.world:8787/login` | Sign in / sign up |
| `http://local.friendo.world:8787/dashboard` | Your deployed sites |
| `http://{name}.local.friendo.world:8787` | A deployed site |

The `?site=` param still works as a fallback (e.g. `http://localhost:8787/?site=testsite`).

## Scripts

| Command | Description |
|---|---|
| `npm run build` | Compile the Go binary |
| `npm run serve` | Build + serve testsite on :3000 |
| `npm run serve:open` | Same but skip admin auth |
| `npm run clean` | Remove build artifacts + dist/ |
| `npm run init` | Scaffold a new site |
| `npm run export` | Static export testsite to dist/ |
| `npm run deploy:local` | Deploy testsite to local Worker |
| `npm run deploy:dry` | Dry-run deploy |
| `npm run world:install` | Install Worker dependencies |
| `npm run world:dev` | Start Worker on :8787 |
| `npm run world:sync` | Sync testsite to local D1/R2 |
| `npm run world:deploy` | Deploy Worker to production |

## Templates

Templates use [Pongo2](https://github.com/flosch/pongo2) (Jinja2-compatible). Available context:

- `{{ site.name }}` — the site name
- `{{ request.path }}` — the current request path
- `{{ collections.blog }}` — all posts in the "blog" collection
- `{{ record }}` — the matched record on dynamic routes

## Advanced sync options

```bash
cd world

# Sync a different site directory
bash sync.sh /path/to/my-site

# Sync with a custom site-id
bash sync.sh --site-id my-blog ../testsite

# Sync to production (requires Cloudflare account + wrangler.toml configured)
bash sync.sh --remote --site-id my-blog /path/to/my-site

# Apply schema only (no data sync)
bash sync.sh --schema-only
```
