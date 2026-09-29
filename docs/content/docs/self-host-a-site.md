---
title: Self-host a site
slug: self-host-a-site
section: Guides
topic: Hosting
weight: 20
description: Run one site on a server you own.
---

One site, one server, the same binary you run on your laptop. This is the
smallest possible deployment: no network, no accounts beyond the site's own.

## What you need

- A server (any Linux box) with a hostname pointing at it: `mysite.example.com`.
- The `friendo` binary on it ([Install](/docs/installation)).
- Something in front to terminate HTTPS: [Caddy](https://caddyserver.com) is the
  least work; nginx or Traefik are fine.

## Run it

Copy your site folder up (git, rsync, scp: it's just a folder), then:

```bash
cd /srv/my-site
RESEND_API_KEY=… FRIENDO_EMAIL_FROM='My Site <hello@example.com>' friendo serve --port 3000
```

Wrap that in a `systemd` unit so it restarts on boot. Then point your proxy at it;
with Caddy the whole config is:

```
mysite.example.com {
    reverse_proxy localhost:3000
}
```

Caddy gets the certificate on its own.

## The first sign-in

Open `https://mysite.example.com/_/`. Because the request isn't from the server
itself, the admin asks you to **set up the owner**: your email, then the code it
emails you. That's your account. If the email provider isn't working yet, this
first code is also printed in the terminal where `friendo serve` is running.

> **Warning:** set an email provider before anyone else can reach the site.
> With no `RESEND_API_KEY`, `friendo serve` assumes it's on your laptop and hands
> every sign-in code back to the browser that asked for it, so anyone who knows a
> member's email can sign in as them, the owner included. Set `RESEND_API_KEY`
> and `FRIENDO_EMAIL_FROM` (the command above does), or at the very least
> `FRIENDO_OTP_ECHO=0`. See [Send real email](/docs/email).

## Keeping it updated

From your laptop:

```bash
friendo push --target https://mysite.example.com          # templates + assets
friendo push --target https://mysite.example.com --posts  # and posts
```

The first push asks for your email and a code (the site's own sign-in), then
caches the session. Put `target = "https://mysite.example.com"` under `[deploy]`
in `friendo.toml` and you can drop the flag.

## Media and backups

Uploads land in `assets/uploads/` on disk by default, or in S3-compatible storage
with the `FRIENDO_S3_*` variables. Back up `data/friendo.db` and `assets/`. The
details are in [Upload images and back up media](/docs/images#back-up-media).

## Want more than one site?

Run a [network](/docs/run-a-network) instead: one process, many sites by
subdomain, an account page for the people whose sites they are.
