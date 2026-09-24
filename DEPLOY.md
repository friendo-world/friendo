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
- **Routing (the one fiddly bit):** three kinds of hostname have to reach this app — the apex
  (`example.com` → operator console), the wildcard (`*.example.com` → tenant sites), and
  **any other hostname** (a tenant's custom domain arrives with *their* domain in the Host
  header, not one of yours). Set the app's domain to your apex, then replace the generated
  Traefik labels under **Advanced → Custom Docker Labels** with this block (Traefik **v3**
  syntax, which is what current Coolify ships):
  ```
  traefik.enable=true
  traefik.http.routers.friendo-https.rule=Host(`example.com`) || HostRegexp(`^[a-z0-9-]+\.example\.com$`)
  traefik.http.routers.friendo-https.entryPoints=https
  traefik.http.routers.friendo-https.tls=true
  traefik.http.routers.friendo-https.service=friendo
  traefik.http.routers.friendo-https.priority=100
  traefik.http.routers.friendo-http.rule=Host(`example.com`) || HostRegexp(`^[a-z0-9-]+\.example\.com$`)
  traefik.http.routers.friendo-http.entryPoints=http
  traefik.http.routers.friendo-http.service=friendo
  traefik.http.routers.friendo-http.priority=100
  traefik.http.routers.friendo-catchall-https.rule=PathPrefix(`/`)
  traefik.http.routers.friendo-catchall-https.entryPoints=https
  traefik.http.routers.friendo-catchall-https.tls=true
  traefik.http.routers.friendo-catchall-https.service=friendo
  traefik.http.routers.friendo-catchall-https.priority=50
  traefik.http.routers.friendo-catchall-http.rule=PathPrefix(`/`)
  traefik.http.routers.friendo-catchall-http.entryPoints=http
  traefik.http.routers.friendo-catchall-http.service=friendo
  traefik.http.routers.friendo-catchall-http.priority=50
  traefik.http.services.friendo.loadbalancer.server.port=3000
  ```
  The `catchall` routers are what make custom domains work: without them Traefik answers a
  tenant's domain itself (a bare 404, or a `503 no available server` if a stale rule from
  another resource matches first) and friendo never sees the request. The explicit
  priorities pin the order — your own hostnames take the specific routers, everything else
  falls through to friendo, which serves the site if the domain is verified and, if not,
  a page explaining that the domain isn't connected (or isn't verified yet) and what to do. (Traefik's default priority is the rule's character length,
  so an unpinned catch-all can lose to a leftover rule.) Coolify regenerates these labels if
  you later change the app's domain in its UI, so re-add the block if you do.

  **Check it from your laptop** by sending the box a hostname it has never heard of:
  ```sh
  curl -sk -o /dev/null -w "HTTP %{http_code}\n" \
    --resolve not-a-real-domain.example:443:<server-ip> https://not-a-real-domain.example/
  ```
  You want a **404 from friendo** — its own page, starting `Nothing is connected to
  not-a-real-domain.example`, with instructions. Traefik's plain `404 page not found` body,
  or a **503 no available server**, means the catch-all isn't winning yet and the request
  never reached friendo. Once a tenant verifies a domain, that same path serves their site
  instead.

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

# Custom domains via Cloudflare for SaaS — OPTIONAL. With these set, a tenant running
# 'friendo domain add theirdomain.com' gets a Cloudflare custom hostname, and Cloudflare
# validates ownership and issues + renews the certificate. Without them the network falls
# back to verifying a TXT record itself and leaves TLS to whatever is in front.
# The API token needs Zone → SSL and Certificates → Edit and Zone → Zone → Read.
FRIENDO_CF_API_TOKEN=...
FRIENDO_CF_ZONE_ID=...
# Where Cloudflare sends custom-hostname traffic: a PROXIED record in your zone pointing
# at this network. Set at boot, so there's no dashboard step to forget.
FRIENDO_CF_FALLBACK_ORIGIN=origin.example.com
# What tenants CNAME to. Defaults to the fallback origin, which is usually right —
# set it only if you publish a separate target like cname.example.com.
# FRIENDO_CF_CNAME_TARGET=cname.example.com
# Certificate validation. "http" (default) needs nothing from the tenant but the CNAME;
# "txt" adds records so they can pre-validate before moving live traffic.
# FRIENDO_CF_SSL_METHOD=http
```

### Setting up Cloudflare for SaaS

One-time, on the `example.com` zone:

1. **Enable Custom Hostnames** — dashboard → **SSL/TLS → Custom Hostnames**. It's a paid
   add-on billed per active custom hostname; check the current price before opening it to
   tenants, since each connected domain is a recurring cost.
2. **Add a proxied origin record** — `origin.example.com  A  <server-ip>`, orange cloud on.
   This is what `FRIENDO_CF_FALLBACK_ORIGIN` names, and the network sets it as the zone's
   fallback origin on boot (watch for the `fallback origin …` line in the logs).
3. **Create the API token** — My Profile → API Tokens → Custom token, scoped to this zone:
   *Zone → SSL and Certificates → Edit* and *Zone → Zone → Read*. It deliberately does
   **not** need DNS edit — friendo never writes DNS records, so a leaked token can't
   repoint your zone.

A tenant then runs `friendo domain add theirdomain.com`, adds the single CNAME they're
given, and runs `friendo domain verify theirdomain.com`. Cloudflare handles ownership
validation and the certificate; nothing is installed on the box.

Two things that live outside Cloudflare and are easy to miss:

- **Traefik must pass unknown hostnames through to friendo.** Cloudflare forwards a
  custom hostname to your fallback origin with the *tenant's* domain in the Host header.
  `friendo domain verify` succeeds regardless (it talks to the Cloudflare API), but the
  domain then serves a Traefik 404/503 unless the `catchall` routers from
  [section 3](#3-coolify-app) are in place. Run the curl check there before your first real
  domain.
- **Origin TLS for foreign hostnames.** Traefik presents your Origin CA cert (issued for
  `example.com` + `*.example.com`) for any SNI, and Cloudflare trusts Origin CA. If a
  connected domain shows a Cloudflare **526** page, the zone's **Full (strict)** mode is
  refusing that cert for a name it doesn't cover — drop the zone to **Full** and retry.

The same Traefik point applies **without** Cloudflare for SaaS: under the default TXT
verification, a custom domain still reaches the box with its own hostname, so the
catch-all is required either way.

> Sign-in is passwordless everywhere: the operator console, `friendo login`, and member
> login all deliver a one-time code by email — so `RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`
> are required on a live network (without them, no one can receive a code).

> **Limits.** Each account can create **3** sites by default, so opening signups can't run
> up an unbounded bill. Change it with `friendo network quota --default <n>`, or lift it for
> one tenant with `friendo network quota <email> <n>`. Operators are never limited.
> See `friendo network accounts` for who's using what, and `friendo network accounts suspend`
> / `friendo network sites suspend` for the reversible alternative to `destroy`.

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
5. **Custom domains (if configured):** with a domain you control,
   ```sh
   cd my-site && friendo domain add yourtest.com --network https://example.com
   # add the CNAME it prints at your registrar, wait for it to resolve, then
   friendo domain verify yourtest.com --network https://example.com
   ```
   `https://yourtest.com` should serve `demo` over HTTPS with a Cloudflare-issued
   certificate. Before `verify`, the same URL shows a friendo page saying the domain isn't
   live yet and listing the remaining steps — that's expected. A Traefik `404 page not found` or
   `503 no available server` means the catch-all routers in section 3 are missing; a
   Cloudflare 526 means the origin TLS mode note above applies. Finish with
   `friendo domain remove yourtest.com` so the test hostname stops billing.

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
