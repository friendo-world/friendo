---
title: Try the community features
slug: community-features
status: published
date: 2026-07-08
location:
  lat: 40.6892
  lng: -74.0445
  label: Statue of Liberty
poll:
  slug: cats-or-dogs
  question: Cats or dogs?
  options: [Cats, Doggos]
---

Friendo ships **comments, reactions, polls, and maps** as drop-in Web Components —
no custom JavaScript required. This post has all of them wired up below. The
approved comments, reaction tallies, and poll also render **server-side** (straight
from `record.comments` / `record.reactions` / `record.poll`), so they're readable
with JavaScript off — the components add the interactive, signed-in layer on top.

## How it works

1. **Sign in** with your email. Friendo emails you a one-time code (in local dev
   the code is shown right in the form) — no password to remember.
2. **React** to the post with an emoji, or **vote** in the poll.
3. **Comment.** New comments wait in the site's moderation queue until an admin
   approves them (or turn on auto-approve in the admin **Settings**).

Everything you write is tied to your **member** account — a passwordless identity
scoped to this site. Have more than one? Use the **personas** switcher in the
sign-in box to pick which profile your comments post as.

## Styling

Each component renders into a shadow root and exposes its internals through
`part` attributes, so this page themes them with plain CSS via `::part()`. View
source to see how.
