---
title: Auth & users
slug: auth
group: Concepts
weight: 7
---

Every Friendo site — local or deployed — has the same auth model, backed by the
site's own database.

## First run

The first time you open the admin UI (`/_/`), you create the site **owner** with
an email and password. That account owns the site.

## Roles

Each account has a role. Roles are bundles of capabilities; each one adds to the
one below it:

| Role | Can |
|---|---|
| **Owner** | Everything — manage admins and other owners, transfer ownership, destroy the site |
| **Admin** | Manage people (users up to editor), settings, and deploys |
| **Editor** | Create and edit **any** content, publish, moderate all comments |
| **Contributor** | Create and edit **their own** posts, and moderate comments on them |
| **Member** | Comment, react, and vote — no content of their own |

The key distinction is **own vs. any**: a Contributor can edit only what they
authored, while an Editor can edit anyone's content. Owners can have **co-owners**
— the site just always keeps at least one (you can't remove the last owner; to
step down, promote someone else first).

## Members: passwordless visitors

**Members** are visitors who verify their email with a one-time code — no
password. They get a `member` account: authenticated enough to comment, react, and
vote, but with no content of their own. This powers the community features.

Passwordless works for privileged roles too: if you promote a member to
contributor/editor/admin, they keep signing in with an email code. (For an
admin-level account, that means their email security is the site's security —
configure a real email provider in production.)

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

Owners and admins add or promote accounts anytime from the admin UI.

## Profiles

An account (`users` row) is the auth identity. Display **profiles** live in
`authors` — one account can have several — and all content references a profile.
Every account gets a default profile on creation.

## Portable accounts

Because password hashes are portable, `friendo push --users` carries accounts to a
deployed site unchanged — the same password works everywhere. `friendo pull
--users` brings them back.

## Site auth vs. platform auth

If you host on **friendo.world**, there are two independent auth systems:

- **Site auth** — the accounts *in* your site (above), for your site's owner,
  staff, and members.
- **Platform auth** — your friendo.world account, used to manage and provision
  sites. Platform collaborators open a site's admin as an **owner**.

They're separate: signing into friendo.world doesn't sign you into any site's
admin, and vice versa.
