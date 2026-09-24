---
title: Auth & users
slug: auth
group: Concepts
weight: 7
---

Every friendo site — local or deployed — has the same auth model, backed by the
site's own database. The short version: **people sign in with a code sent to
their email.** There is no password unless you turn passwords on.

## On your own machine: nothing to sign in to

`friendo serve` on your laptop opens the admin (`/_/`) without a sign-in at all.
You're the owner. That's on purpose: with no email provider configured there'd
be nowhere to send a code, and a site on `localhost` is yours.

The rule is strict — the admin only opens for a request that comes from the same
machine, to `localhost`, with nothing in front of it. Put the site behind a proxy
or on a real hostname and sign-in is required. To see the sign-in screens locally
anyway, run `friendo serve --require-login`.

It's the *admin* that opens, not the site: on your pages, `<friendo-auth>` still
treats you as a visitor until you sign in with a code (which works locally — the
code shows on the page), and once you're signed in as a member that's who you
are, even in the admin. So community features are testable locally as they'll
behave for real people.

## First run on a server

The first time anyone opens the admin of a site that's actually on the internet,
they create the site **owner**: enter an email, get a 6-digit code, enter the
code. No account exists until the code checks out, so an unclaimed site can't be
grabbed by a bot that gets there first.

If the site has no email provider yet, the code is also printed in the terminal
running `friendo serve` — so you can finish setup from the server.

A site you publish with `friendo deploy` never shows this screen: the network
already made you its owner.

## Signing in

The admin's sign-in screen asks for your email and sends a code. That's it, for
every role — owners and editors included.

**Allowing passwords.** In admin **Settings → Signing in**, turn on *Allow
signing in with a password*. The sign-in screen then also offers "Use a password
instead", and the user form gets an optional password field. Codes keep working
for everyone regardless. (Setting a password at first-run setup — what an older
CLI does — turns this on for you.)

For an owner or admin who signs in by code, their email account's security *is*
the site's security. On a production site, configure a real email provider
(`RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`) — without one, sign-in codes can only
reach the server log.

## Roles

Each account has a role. Roles are bundles of capabilities; each one adds to the
one below it:

| Role | Can |
|---|---|
| **Owner** | Everything — manage admins and other owners, transfer ownership |
| **Admin** | Manage people (users up to editor), settings, and deploys |
| **Editor** | Create and edit **any** content, publish, moderate all comments |
| **Contributor** | Create and edit **their own** posts, and moderate comments on them |
| **Member** | Comment, react, and vote — no content of their own |

The key distinction is **own vs. any**: a Contributor can edit only what they
authored, while an Editor can edit anyone's content. Owners can have **co-owners**
— the site just always keeps at least one (you can't remove the last owner; to
step down, promote someone else first). Deleting a site altogether is an
operator's job on the network, not a site role.

## Members: visitors who verify their email

**Members** are visitors who sign in through a `<friendo-auth>` tag on your
pages — same email code, no password. They get a `member` account: authenticated
enough to comment, react, and vote, but with no content of their own. This
powers the [community features](/docs/community).

Promote a member to contributor, editor or admin and nothing changes about how
they sign in.

Your pages know who's signed in, too: `{{ user.name }}` renders in a template, and
`{% members only %}` at the top of a page (or `editors only`, `admins only`, …)
keeps it for the people it's meant for — visitors see your `login.html` instead.
See [Members-only pages](/docs/templates#members-only-pages).

## Access presets

Different sites want different defaults. In admin **Settings → Access & roles**,
pick a preset (or fine-tune the pieces):

| Preset | New sign-ups become | Sign-ups | Posts need approval |
|---|---|---|---|
| **Personal** | — | off | — |
| **Community** | Contributor (can post) | on | off |
| **Blog** | Member (comment only) | on | on |

Three settings sit under the presets:

- **New members can post** — whether a fresh sign-up is a Member or a Contributor.
- **Allow sign-ups** — whether visitors can self-register at all.
- **Posts need approval** — when on, a Contributor's posts start as drafts and only
  go live once an Editor publishes them. (Unpublished posts never show on the
  public site.)

Owners and admins add or promote accounts anytime from the admin UI. Adding
someone needs only their email — they sign in with a code.

## Profiles

An account (`users` row) is the auth identity. Display **profiles** live in
`authors` — one account can have several — and all content references a profile.
Every account gets a default profile on creation.

## Portable accounts

`friendo push --users` carries accounts to a deployed site, password hashes
included, so a password set locally works on the deployed site too (if it allows
passwords). `friendo pull --users` brings them back.

## Site auth vs. your network account

If you host on a **network** like friendo.world, there are two independent things
called "signing in":

- **Site auth** — the accounts *in* your site (above): its owner, staff, and
  members.
- **Your network account** — your account *on* the network, which owns your
  sites. Also an email code (`friendo login` runs the same flow from the
  terminal). Running a network is an account **capability** called **operator**.

They stay separate, but the network bridges them for you: `friendo deploy` mints
your site's admin session from your network sign-in, and the **Open admin**
button on [your account page](/docs/network-account) does the same in the
browser — so you sign in once, not twice.
