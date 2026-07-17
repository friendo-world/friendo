# Deploying a friendo network (Coolify)

Network mode runs **one friendo container** that hosts many tenant sites by subdomain
(`friendo network serve`). This guide deploys it on **Coolify** with **Cloudflare** in front
and **Cloudflare R2** for media. Reference target: **Vultr Chicago**, but any Coolify host works.

> One binary, two modes: `friendo serve` (one site) vs `friendo network` (many sites, one
> operator). friendo.world is just the reference network — the same image below.

## What you need first

- A **domain** on Cloudflare (DNS managed by Cloudflare).
- An **R2 bucket** + an S3 API token (Access Key ID + Secret) — media is offloaded here so the
  server's disk only holds SQLite DBs + site folders.
- This **repo** connected to Coolify (Coolify builds the [`Dockerfile`](Dockerfile) on deploy).

## 1. Server + Coolify

1. On **Vultr**, deploy the **Coolify** one-click Marketplace app in **Chicago (ORD)** —
   2 vCPU / 4 GB / 100 GB NVMe (~$24/mo). Turn on **automatic backups**.
2. Firewall: expose only **80/443** publicly. Reach the Coolify dashboard (**:8000**) over an
   SSH tunnel or restrict it to your IP.

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

# First operator — auto-created on first boot (turnkey). Remove after first deploy if you like.
FRIENDO_OPERATOR_EMAIL=you@example.com
FRIENDO_OPERATOR_PASSWORD=<a strong password>

# Optional: real email for passwordless (OTP) member login
RESEND_API_KEY=...
FRIENDO_EMAIL_FROM=friendo <noreply@example.com>
```

Deploy.

## 4. Verify

1. Visit **`https://example.com`** → the operator console (sign in with the bootstrap operator).
2. Create a site `demo` in the console → **`https://demo.example.com`** serves it.
3. From your laptop, deploy a real local site in one command:
   ```sh
   friendo network login  https://example.com          # once
   cd my-site && friendo network deploy demo --network https://example.com
   ```
4. Upload an image in the site's admin and confirm the object appears in your **R2 bucket**
   (first real R2 smoke). Confirm the admin SPA loads and the site serves over HTTPS.

Operators can also be managed from the box: `docker exec <container> friendo network --root
/data/network operator add someone@example.com`.

## 5. Backups

- **Vultr auto-backups/snapshots** cover the whole `/data` volume (all tenant DBs) — turn them on.
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
