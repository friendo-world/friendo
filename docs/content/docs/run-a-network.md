---
title: Run a network
slug: run-a-network
group: Hosting
weight: 31
---

A **network** is one `friendo` process hosting many sites by subdomain — the
thing friendo.world is. Anyone can run one: for a club, a class, a company, or
just their own handful of sites. This page is the shape of it; the step-by-step
hosting recipe (Coolify + Hetzner + Cloudflare) lives in the repo's
[DEPLOY.md](https://github.com/friendo-world/friendo/blob/main/DEPLOY.md).

## Start it

```bash
FRIENDO_BASE_DOMAIN=example.com friendo network serve --root /data/network
```

Every site is a folder under `--root`, served at `<folder>.example.com`. DNS
needs two records pointing at the box: the bare domain and a wildcard `*`.
Whatever terminates HTTPS in front must pass **every** hostname through — the
apex, the wildcard, and (for custom domains) names that aren't yours at all.

Try it locally with no DNS at all: `friendo network serve --base-domain localhost`
puts sites at `demo.localhost:3000`, and browsers resolve `*.localhost` on
their own.

## The first operator

An **operator** is an account with the power to run the network. Sign-in is an
emailed code, so set an email provider (`RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`).
Then either:

- set `FRIENDO_OPERATOR_EMAIL=you@example.com` before starting, or
- just sign in at `https://example.com/account` — with no operator yet, the first
  person to sign in becomes one.

For local dev, `FRIENDO_OTP_ECHO=1` shows the code on the page instead of
emailing it.

## The console

`https://example.com/network` is the operator's page: a single
`<friendo-console>` tag with everything you can do —

- **Sites** — create (naming an owner), put on hold, hand to someone else, delete.
- **Who can join** — invite-only (the default) or open to any email.
- **Site limit** — how many sites one account may make (3 by default), and
  per-person exceptions.
- **People** — suspend, let back in, sign out everywhere, make or unmake operators.
- **Invites** — with an expiry; revoke; forget the expired ones.
- **Custom domains** — on any site, with the DNS records to hand a tenant.
- **Home site** — see below.

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
bare domain). It's an ordinary site — its admin is at `example.com/_/`, it has
its own owner, you push to it like any other.

Four paths on the bare domain are the network's own **default pages**, each a
shell around one tag:

| Path | Tag | For |
|---|---|---|
| `/account` (and `/login`) | `<friendo-account>` | people's sites, domains, Open admin |
| `/network` | `<friendo-console>` | operators |
| `/activate` | `<friendo-activate>` | approving `friendo login` from the terminal |

A home site **takes one over by defining that page itself**: put
`pages/account.html` in the `www` site with `<friendo-account></friendo-account>`
in it, and `example.com/account` is your page around the network's component.
Leave a path undefined and the network's plain default keeps serving there.
`/api/*` is always the network's — the tags talk to it — so a home site can't
have pages under `/api/`.

Some subdomains are **reserved** for the network — `www`, `docs`, `api`, `admin`,
`mail`, `origin`, `network`, `console`, `account`, `login`, `activate`, `static`,
`cdn`. Tenants can't claim them; operators create them on purpose.

## Everything is a setting

| Variable | What |
|---|---|
| `FRIENDO_BASE_DOMAIN` | The network's domain (`example.com`) |
| `FRIENDO_NETWORK_ROOT` | Where site folders live (`/data/network`) |
| `FRIENDO_PORT` | Port to listen on (3000) |
| `FRIENDO_OPERATOR_EMAIL` | Bootstrap operator |
| `RESEND_API_KEY`, `FRIENDO_EMAIL_FROM` | Email delivery — required on a live network |
| `FRIENDO_S3_ENDPOINT`, `FRIENDO_S3_BUCKET`, `FRIENDO_S3_ACCESS_KEY`, `FRIENDO_S3_SECRET_KEY`, `FRIENDO_S3_REGION` | Media in S3-compatible storage (R2); disk otherwise |
| `FRIENDO_CF_API_TOKEN`, `FRIENDO_CF_ZONE_ID`, `FRIENDO_CF_FALLBACK_ORIGIN`, `FRIENDO_CF_CNAME_TARGET`, `FRIENDO_CF_SSL_METHOD` | [Custom domains](/docs/custom-domains) via Cloudflare for SaaS; a TXT check otherwise |

Signup policy, the site limit and the home site are stored in the network itself
and changed from the console or CLI — no restart.

## What operators can and can't do

Operators run the **network**: sites' existence, who joins, limits, domains. They
don't get into a site's content — the "Open admin" button only works for a
site's own owner, operators included. Reversible levers first: suspend an
account, put a site on hold. `destroy` is the irreversible one and asks twice.
