---
title: Auth & users
slug: auth
group: Concepts
weight: 7
---

Every Friendo site — local or deployed — has the same auth model, backed by the
site's own database.

## First run

The first time you open the admin UI (`/_/`), you create a **superadmin** account
with an email and password. That account owns the site.

## Roles

Users have a role, in a strict hierarchy:

| Role | Can |
|---|---|
| **superadmin** | Everything, including managing other admins |
| **admin** | Manage content, users, and settings |
| **editor** | Create and edit content |
| **member** | Authenticate as a visitor (no admin access) |

Superadmins add users from the admin UI. Passwords are bcrypt-hashed and sessions
are stored in the database.

## Members (passwordless visitors)

**Members** are visitors who verify their email with a one-time code — no password.
They get a `member` account: authenticated enough to comment or react, but with no
access to admin endpoints. This powers community features on a site.

## Portable accounts

Because password hashes are portable, `friendo push --users` carries accounts to
a deployed site unchanged — the same password works everywhere. `friendo pull
--users` brings them back.

## Site auth vs. platform auth

If you host on **friendo.world**, note there are two independent auth systems:

- **Site auth** — the accounts *in* your site (above), for your site's admin and
  members.
- **Platform auth** — your friendo.world account, used to manage and provision
  sites.

They're separate: signing into friendo.world doesn't sign you into any site's
admin, and vice versa.
