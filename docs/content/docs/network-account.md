---
title: Your network account
slug: network-account
group: Hosting
weight: 33
---

Publishing to a network — friendo.world, or one somebody else runs — gives you an
**account** there. It's separate from the accounts inside your site: it's the
thing that *owns* your sites.

## Signing in

Two doors into the same account:

- **In a browser:** `https://friendo.world/account` (or `/login`). Enter your
  email, get a code, enter the code.
- **From the terminal:** `friendo login`. It opens the browser to the same
  sign-in with a short device code; once you approve, the CLI is signed in for
  a month. `friendo deploy` does this for you the first time.

There's no password. Networks are invite-only by default, so the first time you
sign in the operator has to have invited you — the message says so if not.

## Your sites

The account page lists every site you own on the network, with:

- **Open admin** — signs you straight into that site's admin (`/_/`). No second
  sign-in: the network mints the session and hands your browser a one-time link.
  From the terminal, `friendo open-admin` does the same.
- **Domains** — connect your own domain, see the DNS records to add, check
  whether it's live, or disconnect it. See [Custom domains](/docs/custom-domains).
- **New site** — claim a subdomain right there, then push a folder to it with
  `friendo deploy that-name`.

It also shows how many sites you can still make. Every account has a limit (3 on a
fresh network); when you hit it, the message says who can raise it.

Some names are reserved — `www`, `docs`, `api`, `admin` and a few others belong
to the network itself. Pick another.

## Suspended?

An operator can suspend an account or put a site on hold. If that happens you'll
be told when you try to sign in or deploy, with the reason they gave. Nothing is
deleted; talk to the operator.

## On a home site

The account page at `/account` is the network's own page — a plain shell around
one tag, `<friendo-account>`. A network whose bare domain shows a real site (a
[home site](/docs/run-a-network#the-home-site)) can wrap that same tag in its own
design. friendo.world does exactly that.
