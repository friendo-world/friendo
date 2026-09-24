---
title: Self-host a site
slug: self-host-a-site
group: Hosting
weight: 30
---

One site, one server, the same binary you run on your laptop. This is the
smallest possible deployment: no network, no accounts beyond the site's own.

## What you need

- A server (any Linux box) with a hostname pointing at it — `mysite.example.com`.
- The `friendo` binary on it ([Installation](/docs/installation)).
- Something in front to terminate HTTPS: [Caddy](https://caddyserver.com) is the
  least work; nginx or Traefik are fine.

## Run it

Copy your site folder up (git, rsync, scp — it's just a folder), then:

```bash
cd /srv/my-site
RESEND_API_KEY=… FRIENDO_EMAIL_FROM='My Site <hello@example.com>' friendo serve --port 3000
```

Wrap that in a `systemd` unit so it restarts on boot. Then point your proxy at it —
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
emails you. That's your account. If you didn't set an email provider, the code
is printed in the terminal where `friendo serve` is running.

> **Set an email provider.** With `RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`, sign-in
> codes reach people's inboxes. Without one, only the server log sees them — fine
> for you, useless for members who want to comment. Never set `FRIENDO_OTP_ECHO`
> on a public site; that flag hands codes back in API responses for local dev.

## Keeping it updated

From your laptop:

```bash
friendo push --target https://mysite.example.com          # templates + assets
friendo push --target https://mysite.example.com --data   # and content
```

The first push asks for your email and a code (the site's own sign-in), then
caches the session. Put `target = "https://mysite.example.com"` under `[deploy]`
in `friendo.toml` and you can drop the flag.

## Media and backups

Uploads land in `assets/uploads/` on disk by default; set `FRIENDO_S3_*` to keep
them in S3-compatible storage instead ([Run a network](/docs/run-a-network)
lists the variables — they're the same for one site). Back up the folder:
`data/friendo.db` (SQLite, WAL mode — copy it with `sqlite3 .backup` or stop the
server first) and `assets/`.

## Want more than one site?

Run a [network](/docs/run-a-network) instead: one process, many sites by
subdomain, an account page for the people whose sites they are.
