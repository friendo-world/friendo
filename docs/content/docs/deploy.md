---
title: Put it on the internet
slug: deploy
section: Tutorials
weight: 50
description: One command puts the folder on friendo.world. Five minutes.
---

Your site is a folder on your machine. This tutorial puts that folder on the
internet with `friendo deploy`, which hosts it on **friendo.world** at a
subdomain of your choosing. There are other ways to host, and they use the same
binary; this is the one with the fewest steps.

## 1. Pick a name

```bash
cd my-site
friendo deploy my-club
```

`my-club` becomes `https://my-club.friendo.world`. Leave the name off and friendo
makes one from your site's name. Some names (`www`, `docs`, `api`, `admin`) are
the network's own; pick another if it says so.

## 2. Sign in

The first time, `deploy` opens your browser to sign you in: enter your email,
enter the code it sends. No password. That's your **network account**, the thing
that owns your sites, and it's separate from the members inside your site. The
terminal picks up where you left off.

> **Note:** friendo.world is invite-only by default. If the sign-in page says
> you haven't been invited, that's the operator's call, not a bug.

## 3. Watch it go

`deploy` claims the subdomain for your account, pushes your templates and assets,
and saves the target in `friendo.toml`:

```toml
[deploy]
target = "https://my-club.friendo.world"
```

Open the address it prints. Your site is live, with its own database and asset
storage. Its admin is at `/_/`, and `friendo deploy` already made you its owner,
so there's no first-run setup screen.

## 4. Push a change

Edit a template, then:

```bash
friendo push            # templates and assets
friendo push --posts    # also the posts you wrote locally
friendo push --users    # also accounts and profiles
```

A pushed stylesheet or image is live at once. What travels when, and how to pull
changes back down, is in [Keep a deployed site in sync](/docs/push-and-pull).

## 5. Find it again

[friendo.world/account](https://friendo.world/account) lists your sites. From
there you can open a site's admin (no second sign-in), connect
[your own domain](/docs/custom-domains), or claim another subdomain. From the
terminal, `friendo open-admin my-club` does the same.

> **What you built.** A site on the internet, owned by your network account,
> that you update from your laptop with one command. friendo.world is convenient,
> not required: everything it does you can do on your own server.

**Next:** [Keep a deployed site in sync](/docs/push-and-pull). Rather host it
yourself? [Self-host a site](/docs/self-host-a-site) runs one site on your own
server, [Run a network](/docs/run-a-network) runs many, and
[Export a static site](/docs/static-export) needs no server at all.
