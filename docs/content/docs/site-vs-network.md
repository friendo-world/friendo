---
title: Your site and the network
slug: site-vs-network
section: Concepts
weight: 60
description: Why a hosted site has two sign-ins, one for its members and one for the network account that owns it.
---

A friendo site stands on its own. It has its own database, its own members and
its own admin, whether it runs on your laptop, on a server you rent, or on
friendo.world. When you host it on a **network** (friendo.world, or one somebody
else runs), a second thing appears: your **network account**, the account *on*
the network that owns your sites.

## Two sign-ins, kept apart

**Your site's members** are the people inside one site: its owner, its staff and
its community. They sign in to that site, with a code sent to their email, and
their roles decide what they can do there. See [Signing in](/docs/signing-in).

**Your network account** is who you are to the network. It doesn't write posts or
moderate comments. It owns sites: it claims subdomains, connects domains and opens
admins. It signs in with an emailed code too, and there's no password.

The two stay separate on purpose. A site can move to another network, or to your
own server, and take its members with it, because nothing about them lives in the
network. And a network operator who runs the machine doesn't become a member of
every site on it.

## Two doors into the network account

In a browser, the account lives at `/account` on the network's own domain
(`https://friendo.world/account`, or `/login`): enter your email, then the code.

From the terminal, `friendo login` opens the browser to the same sign-in with a
short device code. Once you approve it, the CLI stays signed in for a month.
`friendo deploy` does this for you the first time. `friendo whoami` shows who
you're signed in as, and `friendo logout` forgets it.

Networks are invite-only unless the operator opens them. If you sign in before an
operator has invited you, the code works but the message says you haven't been
invited. That's the operator's choice, not a fault.

## Signing in once, not twice

Although the sign-ins are separate, the network bridges them for you. A site made
with `friendo deploy` names your network account as its owner, so there's no
first-run setup screen. After that, the network can vouch for you to the site:
`deploy` signs you into the site's admin from your network sign-in, and the
**Open admin** button on your account page does the same in the browser, handing
your browser a one-time link into the site's `/_/`. `friendo open-admin` is the
terminal twin. It works only for a site's own owner, operators included.

Syncing is the exception that shows the line. `friendo push` and `friendo pull`
talk to the site, not the network, so on a site you host yourself the CLI asks for
the site's own code (or password). See
[Keep a deployed site in sync](/docs/push-and-pull).

## What the account page shows

The account page lists every site you own on the network. Beside each is **Open
admin**, and **Domains**, where you connect your own address, see the DNS records
to add and check whether it's live (see [Custom domains](/docs/custom-domains)).
**New site** claims a subdomain right there; then `friendo deploy that-name` pushes
a folder to it.

It also shows how many more sites you can make. Every account has a limit, 3 on a
fresh network, so opening sign-ups can't cost the operator without bound. When you
reach it, the message says so and that an operator can raise it.

Some names belong to the network itself and can't be claimed: `www`, `docs`,
`api`, `admin`, `mail`, `origin`, `network`, `console`, `account`, `login`,
`activate`, `static` and `cdn`. Operators can still create them, which is how a
network's own sites get those names.

## When an operator steps in

An operator can suspend an account or a site. A suspended account can't sign in,
make sites or use a session it already holds; a suspended site shows visitors a
notice instead of its pages. You're told when you try to sign in or deploy, with
the reason the operator gave. Nothing is deleted, and it can be undone: talk to
the operator.

## The account page on a home site

On a plain network, `/account` is one of the network's built-in pages: a plain
shell around one tag, `<friendo-account>`. A network whose bare domain shows a
real site, its **home site**, can put that same tag inside its own design by
giving the home site a `pages/account.html`. friendo.world does exactly that. The
tag still talks to the network, so the account behind it is the same. See
[Run a network](/docs/run-a-network#the-home-site).
