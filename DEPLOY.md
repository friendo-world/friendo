# Deploying a friendo network (Coolify)

Network mode runs **one friendo container** that hosts many tenant sites by subdomain
(`friendo network serve`). This guide deploys it on **Coolify** with **Cloudflare** in front
and **Cloudflare R2** for media. Reference target: **Hetzner (Ashburn, US)**, but any Coolify host works.

> One binary, two modes: `friendo serve` (one site) vs `friendo network` (many sites, one
> operator). friendo.world is just the reference network — the same image below.

## What you need first

- A **domain** on Cloudflare (DNS managed by Cloudflare).
- An **R2 bucket** + an S3 API token (Access Key ID + Secret) — media is offloaded here so the
  server's disk only holds SQLite DBs + site folders.
- This **repo** connected to Coolify (Coolify builds the [`Dockerfile`](Dockerfile) on deploy).

## 1. Server + Coolify

Reference host: **Hetzner Cloud, Ashburn VA (`ash`)** — Coolify's closest partner, cheapest, and
central-most US option. Pick a **4 GB** type: **CPX21** (3 vCPU / 4 GB) to start, **CPX31**
(4 vCPU / 8 GB) for headroom. (US = CPX/CCX only; the cheaper ARM `cax` types are EU-only.)

Two topologies — **A is the least-friction and recommended.**

### A. Coolify Cloud + Hetzner integration (recommended)
Coolify hosts the dashboard (~$5/mo) and provisions/manages your box for you — no self-hosted
control plane, no port-8000 firewalling, no SSH tunnel.
1. Sign up for **Coolify Cloud** (coolify.io/cloud).
2. Add your **Hetzner Cloud API token** (Hetzner console → Security → API Tokens, **Read & Write**).
3. In Coolify, **provision a server**: Hetzner, **Ashburn (`ash`)**, **CPX21**, Ubuntu 24.04.
   Coolify creates it, installs Docker + its agent, and connects it — no CLI, no SSH keys to wrangle.
4. Enable **Backups** (Hetzner console) and add a **Hetzner Cloud Firewall** allowing **80/443**
   (public) and **22** (Coolify manages SSH). There's **no port 8000** on your box in this model, so
   nothing to tunnel to.

Trade: Coolify holds your Hetzner token + SSH deploy-access — fine for a solo project; no lock-in
(same software; self-host later). Then jump to §2.

### B. Self-host Coolify on your own box (no monthly fee)
You run + secure Coolify yourself.
- **Automated:** [`scripts/provision-hetzner.sh`](scripts/provision-hetzner.sh) — `hcloud` CLI +
  cloud-init that creates the server + firewall and installs Coolify (with a swap file for build
  headroom). Edit the config block at the top, then run it.
- **Manual:** create an Ashburn **CPX21** server with the **Coolify** one-click app (or Ubuntu 24.04
  + `curl -fsSL https://cdn.coollabs.io/coolify/install.sh | bash`), add your SSH key **at creation**,
  enable Backups.
- **Firewall:** allow **22** and **8000** from **your IP**, **80/443** from anywhere; deny the rest.
  ⚠️ Coolify's first-run setup is on **:8000** — if you block it entirely you can't reach setup, so
  keep 8000 open to your IP (or SSH-tunnel: `ssh -L 8000:localhost:8000 -L 6001:localhost:6001 root@<ip>`).

## 2. Cloudflare (DNS + wildcard TLS)

1. **DNS** (both proxied / orange-cloud):
   - `A  @  → <server IP>`   (apex → operator console)
   - `A  *  → <server IP>`   (wildcard → every tenant site, resolves instantly)
2. **TLS** — HTTP-01 can't issue wildcard certs behind the proxy, so pick one:
   - **Recommended: Cloudflare Origin CA cert.** Create an Origin Certificate for `example.com`
     and `*.example.com`, install it as Traefik's default cert in Coolify, and set the zone's
     SSL/TLS mode to **Full (strict)**. No ACME, no rate limits.
   - **Alternative: DNS-01 wildcard** via a Cloudflare API token (if your Coolify/Traefik is set
     up for the Cloudflare DNS challenge).

## 3. Coolify app

Create an application from this repo (build pack: **Dockerfile**).

- **Build context:** **Base Directory = `/`** and **Dockerfile Location = `/Dockerfile`** — the
  [`Dockerfile`](Dockerfile) is at the repo root (single-app repo, no subfolder), so the base path
  is just the root.
- **Persistent storage:** mount a volume at **`/data`** (tenant folders + SQLite DBs live here;
  survives redeploys).
- **Port:** the container listens on **3000**; let Coolify's Traefik route to it (don't publish
  the port directly).
- **Domains / wildcard routing (the one fiddly bit):** route **both** the apex (`example.com` →
  operator console) **and** the wildcard (`*.example.com` → tenant sites) to this app. Set the
  app's domain to your apex and add a wildcard route. Depending on your Traefik version, that's a
  custom label like:
  ```
  traefik.http.routers.friendo.rule=Host(`example.com`) || HostRegexp(`{sub:[a-z0-9-]+}.example.com`)
  ```
  (Traefik v3 changed `HostRegexp` syntax — verify against your Coolify's Traefik version.)

### Environment variables

```sh
# Network identity
FRIENDO_BASE_DOMAIN=example.com
FRIENDO_NETWORK_ROOT=/data/network      # already the image default
FRIENDO_PORT=3000                       # already the image default

# Media → Cloudflare R2 (S3-compatible). Without these, media stays on the /data volume.
FRIENDO_S3_ENDPOINT=https://<ACCOUNT_ID>.r2.cloudflarestorage.com
FRIENDO_S3_BUCKET=friendo-media
FRIENDO_S3_ACCESS_KEY=...
FRIENDO_S3_SECRET_KEY=...
FRIENDO_S3_REGION=auto

# First operator — this email is granted the operator capability on boot. Sign-in is
# passwordless (email OTP), so no password here. (If you omit this, the first person to
# sign in at the apex console claims operator.)
FRIENDO_OPERATOR_EMAIL=you@example.com

# Email delivery — REQUIRED for sign-in (operators + members receive their OTP by email).
RESEND_API_KEY=...
FRIENDO_EMAIL_FROM=friendo <noreply@example.com>
```

> Sign-in is passwordless everywhere: the operator console, `friendo login`, and member
> login all deliver a one-time code by email — so `RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`
> are required on a live network (without them, no one can receive a code).

Deploy.

## 4. Verify

1. Visit **`https://example.com`** → the operator console. Sign in with your operator email;
   it emails you a one-time code (no password). The `FRIENDO_OPERATOR_EMAIL` account is already
   an operator; if you didn't set one, the first sign-in claims it.
2. Create a site `demo` in the console → **`https://demo.example.com`** serves it.
3. From your laptop, publish a real local site:
   ```sh
   friendo login https://example.com               # once — browser sign-in (device auth)
   cd my-site && friendo deploy demo --network https://example.com
   ```
4. Upload an image in the site's admin and confirm the object appears in your **R2 bucket**
   (first real R2 smoke). Confirm the admin SPA loads and the site serves over HTTPS.

Add another operator from the box: `docker exec <container> friendo network --root
/data/network operator grant someone@example.com` — they then sign in passwordless with
`friendo login`.

## 5. Backups

- **Hetzner Backups** (or your host's snapshots) cover the whole box incl. `/data` (all tenant
  DBs) — turn them on.
- Optionally schedule a `tar` of `/data` to R2 for an off-box copy.
- SQLite runs in WAL mode on block/NVMe storage. **Do not** put `/data` on NFS — its `fsync`
  semantics can corrupt SQLite.

## 6. Cutting friendo.world over

De-risk on a throwaway domain first. Then point `friendo.world`'s apex + wildcard at the box,
migrate any tenants, and retire the Cloudflare Workers-for-Platforms stack.

---

**Notes.** The image runs as root so it can write to a freshly-mounted volume without a chown
dance (hardening to non-root is a follow-up). Config is entirely env-driven — an explicit CLI
flag would override the corresponding `FRIENDO_*` var, but in a container you won't pass flags.
