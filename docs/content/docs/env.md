---
title: Environment variables
slug: env
section: Reference
weight: 90
description: Every environment variable friendo reads, what it does, its default, and whether it applies to a site, a network or the CLI.
---

Set them in the shell before the command, or in your host's environment settings:

```bash
RESEND_API_KEY=re_… FRIENDO_EMAIL_FROM='My Site <hello@example.com>' friendo serve
```

A command-line flag always wins over the matching variable.

## Email

| Variable | What | Default | Applies to |
|---|---|---|---|
| `RESEND_API_KEY` | A [Resend](https://resend.com) API key. Sign-in codes are emailed through it. Needs `FRIENDO_EMAIL_FROM` too | unset: no email is sent | site, network |
| `FRIENDO_EMAIL_FROM` | The sender of sign-in emails: `My Site <hello@example.com>`. Needs `RESEND_API_KEY` too | unset | site, network |
| `FRIENDO_OTP_ECHO` | `1`, `true`, `yes` or `on`: show sign-in codes on the page instead of emailing them. For local testing only. Ignored once both email variables are set | `friendo serve` sets it to `1` when `RESEND_API_KEY` is unset; otherwise off | site, network |

## Signing in

| Variable | What | Default | Applies to |
|---|---|---|---|
| `FRIENDO_REQUIRE_LOGIN` | `1`, `true`, `yes` or `on`: the same as `friendo serve --require-login`. The admin asks for a sign-in even on `localhost` | off | site |

## Media storage

Set the first four and uploads go to an S3-compatible bucket (Cloudflare R2, AWS S3,
MinIO…) instead of `assets/uploads/` on disk. With some but not all four set, media
stays on disk and the startup log warns which are missing.

| Variable | What | Default | Applies to |
|---|---|---|---|
| `FRIENDO_S3_ENDPOINT` | The bucket's endpoint, e.g. `https://<account>.r2.cloudflarestorage.com` | unset: local disk | site, network |
| `FRIENDO_S3_BUCKET` | The bucket name | unset | site, network |
| `FRIENDO_S3_ACCESS_KEY` | Access key id | unset | site, network |
| `FRIENDO_S3_SECRET_KEY` | Secret key | unset | site, network |
| `FRIENDO_S3_REGION` | The bucket's region | `auto` (right for R2) | site, network |

Each site's files are kept under its folder name in the bucket, so many sites can
share one.

## Network

These configure `friendo network` and its subcommands.

| Variable | What | Default | Applies to |
|---|---|---|---|
| `FRIENDO_BASE_DOMAIN` | The network's domain. Sites are served at `<subdomain>.<base domain>`. Same as `--base-domain` | `localhost` | network |
| `FRIENDO_NETWORK_ROOT` | The folder that holds every site's folder and database. Same as `--root`, for every `friendo network` subcommand | `./network` (`/data/network` in the Docker image) | network, CLI |
| `FRIENDO_PORT` | The port `friendo network serve` listens on. Same as `--port` | `3000` | network |
| `FRIENDO_OPERATOR_EMAIL` | This account is made an **operator** on start. On the box, `friendo network provision` also uses it as the new site's owner when `--owner` is not given | unset: the first person to sign in at `/account` becomes operator | network, CLI |

`friendo serve` (one site) takes its port from `--port` only.

## Custom domains (Cloudflare for SaaS)

Optional. With the first two set, a custom domain gets a Cloudflare custom hostname,
and Cloudflare checks ownership and issues the certificate. Without them the network
checks a TXT record itself. See [Custom domains](/docs/custom-domains) and
[Run a network](/docs/run-a-network).

| Variable | What | Default | Applies to |
|---|---|---|---|
| `FRIENDO_CF_API_TOKEN` | A Cloudflare API token with *Zone → SSL and Certificates → Edit* and *Zone → Zone → Read* | unset: TXT check | network |
| `FRIENDO_CF_ZONE_ID` | The zone of the base domain | unset | network |
| `FRIENDO_CF_FALLBACK_ORIGIN` | A proxied record in the zone that points at the network. Set as the zone's fallback origin on start | unset (a warning is printed) | network |
| `FRIENDO_CF_CNAME_TARGET` | What people point their CNAME at | the fallback origin, else the base domain | network |
| `FRIENDO_CF_SSL_METHOD` | Certificate validation: `http`, or `txt` to let people validate before moving traffic | `http` | network |
| `FRIENDO_CF_API_BASE` | Another Cloudflare API address, for pointing a staging network at a mock | the real API | network |

## Time

| Variable | What | Default | Applies to |
|---|---|---|---|
| `TZ` | An IANA zone name. `friendo init` writes it as `timezone` in the new `friendo.toml` | the machine's zone, else `UTC` | CLI |

After `friendo init`, a site's timezone is the `timezone` key in
[friendo.toml](/docs/config), not `TZ`.

## Installer

Read by the one-line installer script (`scripts/install.sh`), not by `friendo` itself.

| Variable | What | Default | Applies to |
|---|---|---|---|
| `FRIENDO_VERSION` | The release tag to install, e.g. `v0.5.0` | the latest release | installer |
| `FRIENDO_INSTALL_DIR` | Where to put the binary | `/usr/local/bin` if writable, else `~/.local/bin` | installer |

```bash
curl -fsSL https://raw.githubusercontent.com/friendo-world/friendo/main/scripts/install.sh | FRIENDO_INSTALL_DIR=~/bin sh
```

## Files the CLI keeps

Not a variable, but where the CLI stores state:

| Path | What |
|---|---|
| `~/.friendo/config` | Your sign-ins: networks from `friendo login`, and the sites you push to |
