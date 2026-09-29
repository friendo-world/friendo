---
title: Run a network
slug: run-a-network
section: Guides
topic: Hosting
weight: 30
description: One host serving many sites by subdomain, like friendo.world.
---

A **network** is one `friendo` process hosting many sites by subdomain, the thing
friendo.world is. Anyone can run one: for a club, a class, a company, or just
their own handful of sites. This page is the shape of it, then
[the recipe](#host-it) friendo.world itself runs on: Coolify, Hetzner and
Cloudflare.

## Start it

```bash
FRIENDO_BASE_DOMAIN=example.com friendo network serve --root /data/network
```

Every site is a folder under `--root`, served at `<folder>.example.com`. DNS
needs two records pointing at the box: the bare domain and a wildcard `*`.
Whatever terminates HTTPS in front must pass **every** hostname through: the
apex, the wildcard, and (for custom domains) names that aren't yours at all.

Try it locally with no DNS at all: `friendo network serve --base-domain localhost`
puts sites at `demo.localhost:3000`, and browsers resolve `*.localhost` on their own.

## The first operator

An **operator** is an account with the power to run the network. Sign-in is an
emailed code, so set an email provider (`RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`).
Then either:

- set `FRIENDO_OPERATOR_EMAIL=you@example.com` before starting, or
- just sign in at `https://example.com/account`: with no operator yet, the first
  person to sign in becomes one.

For local dev, `FRIENDO_OTP_ECHO=1` shows the code on the page instead of emailing it.

## The console

`https://example.com/network` is the operator's page, a single `<friendo-console>`
tag with everything you can do:

- **Sites**: create (naming an owner), suspend, hand to someone else, delete.
- **Who can join**: invite-only (the default) or open to any email.
- **Site limit**: how many sites one account may make (3 by default), and
  per-person exceptions.
- **People**: suspend, let back in, sign out everywhere, make or unmake operators.
- **Invites**: with an expiry; revoke; forget the expired ones.
- **Custom domains**: on any site, with the DNS records to hand the site's owner.
- **Home site**: see below.

Everything there is also a `friendo network …` command, and they work from your
laptop as well as on the box: `friendo login https://example.com` once, then pass
`--network https://example.com`. See the [CLI reference](/docs/cli).

## The home site

By default the bare domain shows a plain page saying this is a friendo network
and where to sign in. Better: make it a real site.

```bash
friendo deploy www --network https://example.com     # a normal friendo site, named www
friendo network home www --network https://example.com
```

Now `example.com` serves the `www` site (and `www.example.com` redirects to the
bare domain). It's an ordinary site: its admin is at `example.com/_/`, it has its
own owner, you push to it like any other.

Three paths on the bare domain are the network's own **built-in pages**, each a
shell around one tag:

| Path | Tag | For |
|---|---|---|
| `/account` (and `/login`) | `<friendo-account>` | people's sites, domains, Open admin |
| `/network` | `<friendo-console>` | operators |
| `/activate` | `<friendo-activate>` | approving `friendo login` from the terminal |

A home site **takes one over by defining that page itself**: put
`pages/account.html` in the `www` site with `<friendo-account></friendo-account>`
in it, and `example.com/account` is your page around the network's tag. Leave a
path undefined and the network's plain page keeps serving there. `/api/*` is
always the network's (the tags talk to it), so a home site can't have pages under
`/api/`.

Some subdomains are **reserved** for the network: `www`, `docs`, `api`, `admin`,
`mail`, `origin`, `network`, `console`, `account`, `login`, `activate`, `static`,
`cdn`. Site owners can't claim them; operators create them on purpose.

## Everything is a setting

| Variable | What |
|---|---|
| `FRIENDO_BASE_DOMAIN` | The network's domain (`example.com`) |
| `FRIENDO_NETWORK_ROOT` | Where site folders live (`/data/network`) |
| `FRIENDO_PORT` | Port to listen on (3000) |
| `FRIENDO_OPERATOR_EMAIL` | The first operator |
| `RESEND_API_KEY`, `FRIENDO_EMAIL_FROM` | Email delivery, required on a live network |
| `FRIENDO_S3_ENDPOINT`, `FRIENDO_S3_BUCKET`, `FRIENDO_S3_ACCESS_KEY`, `FRIENDO_S3_SECRET_KEY`, `FRIENDO_S3_REGION` | Media in S3-compatible storage (R2); disk otherwise |
| `FRIENDO_CF_API_TOKEN`, `FRIENDO_CF_ZONE_ID`, `FRIENDO_CF_FALLBACK_ORIGIN`, `FRIENDO_CF_CNAME_TARGET`, `FRIENDO_CF_SSL_METHOD` | [Custom domains](/docs/custom-domains) via Cloudflare for SaaS; a TXT check otherwise |

Signup policy, the site limit and the home site are stored in the network itself
and changed from the console or CLI; no restart.

## Host it

This is the setup friendo.world uses: one server at Hetzner, managed by Coolify,
with Cloudflare in front for DNS and TLS, Cloudflare R2 for media and Resend for
email. Any host that runs Coolify works. Before you start you need:

- a **domain** whose DNS is on Cloudflare;
- an **R2 bucket** and an S3 API token for it (access key and secret), so the
  server's disk holds only databases and site folders;
- a **Resend** API key, so sign-in codes reach people;
- the friendo repo connected to Coolify, which builds its `Dockerfile` on deploy.

### 1. Get a server with Coolify

The least fuss is **Coolify Cloud** with its Hetzner integration: Coolify hosts
the dashboard and makes the server for you.

1. Sign up for Coolify Cloud and add a **Hetzner Cloud API token** (Hetzner
   console → Security → API Tokens, **Read & Write**).
2. In Coolify, provision a server: Hetzner, Ashburn (`ash`), **CPX21**
   (3 vCPU, 4 GB), Ubuntu 24.04. Coolify installs Docker and connects it.
3. In the Hetzner console, turn on **Backups** and add a firewall allowing ports
   **80** and **443** from anywhere, and **22** for Coolify.

To run Coolify yourself instead, make the server with the Coolify one-click app
and keep port **8000** (Coolify's setup screen) and **22** open to your own IP
only.

### 2. Point DNS at it

In Cloudflare, add two records, both proxied (orange cloud):

| Type | Name | Value | For |
|---|---|---|---|
| `A` | `@` | the server's IP | the bare domain: the home site and the network's own pages |
| `A` | `*` | the server's IP | every site on the network |

### 3. Set up TLS

A wildcard certificate can't be issued the usual HTTP way behind Cloudflare's
proxy. Use a **Cloudflare Origin CA certificate**: create one for `example.com`
and `*.example.com`, install it as Traefik's default certificate in Coolify, and
set the zone's SSL/TLS mode to **Full (strict)**. (A DNS-01 wildcard through a
Cloudflare API token also works, if your Traefik is set up for it.)

### 4. Create the app

In Coolify, create an application from the repo with the **Dockerfile** build
pack.

1. **Build:** Base Directory `/`, Dockerfile Location `/Dockerfile`. The image
   runs `friendo network serve`.
2. **Storage:** mount a persistent volume at **`/data`**. Site folders and their
   SQLite databases live there and survive redeploys. Don't put it on NFS: SQLite
   can be corrupted there.
3. **Port:** the container listens on **3000**. Let Coolify's Traefik route to
   it; don't publish the port.
4. **Routing:** set the app's domain to your bare domain, then replace the
   generated labels under **Advanced → Custom Docker Labels** with these
   (Traefik v3), putting your domain in place of `example.com`:

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

The two `catchall` routers pass every other hostname to friendo, which is what
lets [custom domains](/docs/custom-domains) arrive. Coolify rewrites these labels
if you change the app's domain in its UI, so add them again if you do.

> **Common mistake:** leaving the catch-all out. A custom domain then gets
> Traefik's own `404 page not found` or `503 no available server`, and friendo
> never sees the request.

Check it from your laptop by sending the server a hostname it has never heard of:

```bash
curl -sk -o /dev/null -w "HTTP %{http_code}\n" \
  --resolve not-a-real-domain.example:443:<server-ip> https://not-a-real-domain.example/
```

You want a 404 from friendo, its own page starting `Nothing is connected to
not-a-real-domain.example`. Traefik's plain `404 page not found`, or a 503, means
the catch-all isn't winning yet.

### 5. Set the environment

In the app's environment variables:

```bash
FRIENDO_BASE_DOMAIN=example.com
FRIENDO_NETWORK_ROOT=/data/network      # already the image's default
FRIENDO_PORT=3000                       # already the image's default

# Media in R2. Without these, media stays on the /data volume.
FRIENDO_S3_ENDPOINT=https://<ACCOUNT_ID>.r2.cloudflarestorage.com
FRIENDO_S3_BUCKET=friendo-media
FRIENDO_S3_ACCESS_KEY=...
FRIENDO_S3_SECRET_KEY=...
FRIENDO_S3_REGION=auto

# The first operator. Leave it out and the first person to sign in at /account becomes one.
FRIENDO_OPERATOR_EMAIL=you@example.com

# Email through Resend. Required: sign-in codes go out by email.
RESEND_API_KEY=...
FRIENDO_EMAIL_FROM=friendo <noreply@example.com>
```

Every variable is read when the container starts, none at build time. Then press
**Deploy** in Coolify.

### 6. Custom domains through Cloudflare for SaaS (optional)

Without this step, a site owner's [custom domain](/docs/custom-domains) is checked
with a TXT record and TLS is left to whatever is in front. With it, Cloudflare
checks ownership and issues and renews the certificate, and the owner adds one
CNAME. On the `example.com` zone, once:

1. Turn on **SSL/TLS → Custom Hostnames**. It's a paid add-on billed per active
   custom hostname, so check the price before opening it to site owners.
2. Add a proxied record `origin.example.com  A  <server-ip>`. The network makes it
   the zone's fallback origin when it starts.
3. Make an API token scoped to the zone with *Zone → SSL and Certificates → Edit*
   and *Zone → Zone → Read*. It doesn't need DNS edit: friendo never writes DNS
   records.

Then add to the environment and deploy again:

```bash
FRIENDO_CF_API_TOKEN=...
FRIENDO_CF_ZONE_ID=...
FRIENDO_CF_FALLBACK_ORIGIN=origin.example.com
# FRIENDO_CF_CNAME_TARGET=cname.example.com   # what owners CNAME to; defaults to the fallback origin
# FRIENDO_CF_SSL_METHOD=http                  # or txt, to let owners validate before moving traffic
```

The catch-all routers from step 4 are still needed: Cloudflare sends a custom
domain to your server with that domain as the hostname. If a connected domain
shows a Cloudflare **526** page, Full (strict) is refusing your Origin CA
certificate for a name it doesn't cover; set the zone to **Full** and retry.

### 7. Check it works

1. Open `https://example.com/account` and sign in with the operator email. The
   code arrives by email. `https://example.com/network` is now your console.
2. Make a site called `demo` there. `https://demo.example.com` serves it, and
   **Open admin** on `/account` takes you into its admin.
3. From your laptop, publish a real site to it:

```bash
friendo login https://example.com
cd my-site
friendo deploy demo --network https://example.com
```

4. Upload an image in the site's admin and check it appears in your R2 bucket.

### 8. Back it up

Hetzner's Backups cover the whole server, `/data` included, so keep them on. For
a copy off the server, you can also schedule a `tar` of `/data` to R2.

## Update it

A new version of friendo reaches the network when Coolify builds the image again:
open the app in Coolify and deploy it. Everything on the `/data` volume, every site
and database, stays as it was; only the binary changes.

## What operators can and can't do

Operators run the **network**: sites' existence, who joins, limits, domains. They
don't get into a site's content; the "Open admin" button only works for a site's
own owner, operators included. Reversible levers first: suspend an account,
suspend a site. `destroy` is the irreversible one and asks twice.

The recipe also lives in the repo's
[DEPLOY.md](https://github.com/friendo-world/friendo/blob/main/DEPLOY.md), with
a script for making the server yourself and a checklist for testing a custom domain.
