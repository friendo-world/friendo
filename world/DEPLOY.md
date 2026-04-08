# Edge deployment (M4)

The Friendo edge runtime is a single Cloudflare Worker that serves all sites via subdomain routing. Templates are loaded from R2, content from D1.

## Local development

Local dev uses Wrangler's miniflare to simulate D1 and R2 on your machine. No Cloudflare account, no domain, no internet required.

### 1. Install dependencies

```bash
cd world
npm install
```

### 2. Sync the testsite to local D1 + R2

```bash
bash sync.sh ../testsite
```

This applies the schema, registers the site, syncs any posts from the local SQLite, and uploads templates + assets to the local R2 simulation.

### 3. Start the Worker

```bash
wrangler dev
```

### 4. Visit the site

Open `http://testsite.local.friendo.world:8787` in your browser.

Local dev uses `*.local.friendo.world` — a wildcard DNS record pointing to `127.0.0.1` — so real subdomain routing works on your machine. The friendo.world UI is at `http://local.friendo.world:8787`.

The `?site=` query param also works as a fallback (e.g. `http://localhost:8787/?site=testsite`).

### Re-syncing after changes

After editing templates or content, re-run `sync.sh` and refresh the browser. The Worker picks up changes from D1 and R2 on each request (no caching in dev).

---

## Production setup

One-time steps to stand up the production infrastructure. You need a Cloudflare account with Workers, D1, and R2 enabled.

### 1. Create infrastructure

```bash
cd world

# Create the shared D1 database
wrangler d1 create friendo-world
# Copy the database_id from the output

# Create the R2 bucket for templates and assets
wrangler r2 bucket create friendo-assets
```

### 2. Configure wrangler.toml

Paste the real `database_id` into the `[env.production]` section of `wrangler.toml`, replacing `REPLACE_WITH_REAL_D1_ID`.

### 3. Apply schema to production D1

```bash
bash sync.sh --remote --schema-only
```

### 4. Deploy the Worker

```bash
wrangler deploy --env production
```

This deploys the Worker to Cloudflare's edge. It handles all requests to `*.friendo.world`.

### 5. Configure DNS

In the Cloudflare dashboard for `friendo.world`:

- Add a `CNAME` record: Name `*` (wildcard), Target your Worker's `*.workers.dev` URL
- The route pattern in `wrangler.toml` handles routing `*.friendo.world` to the Worker

### 6. Verify

Deploy a test site and check it's live:

```bash
bash sync.sh --remote --site-id test-site ../testsite
```

Visit `https://test-site.friendo.world`.

---

## Per-site deploy (production)

Once the infrastructure is up, deploying a site is one command:

```bash
bash sync.sh --remote --site-id my-blog /path/to/my-blog
```

The Worker doesn't change — it serves all sites from D1 + R2 dynamically. Only redeploy the Worker (`wrangler deploy --env production`) if you've changed `worker.js`.

---

## Script reference

| Command | Description |
|---|---|
| `bash sync.sh [site-dir]` | Sync to local miniflare |
| `bash sync.sh --remote --site-id NAME [site-dir]` | Sync to production |
| `bash sync.sh --schema-only` | Apply schema only (local) |
| `bash sync.sh --remote --schema-only` | Apply schema only (production) |
| `wrangler dev` | Start Worker locally |
| `wrangler deploy --env production` | Deploy Worker to Cloudflare |
