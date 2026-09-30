---
title: Let visitors take part
slug: visitors
section: Guides
topic: Community
weight: 15
description: Let people react, vote and RSVP without signing in first, and keep what they did when they do sign in.
---

By default, only members can react, vote in a poll or RSVP. A visitor who clicks
gets asked to sign in. You can let visitors do these three things straight away
instead, with one switch each.

## Turn it on

In admin **Settings**, turn on **Visitors can react**, **Visitors can vote** or
**Visitors can RSVP**. Or set them in `friendo.toml`:

```toml
[settings]
visitors_can_react = true
visitors_can_vote = true
visitors_can_rsvp = true
```

Your pages don't change. `<friendo-reactions>`, `<friendo-poll>` and
`<friendo-rsvp>` notice the switch and let visitors click.

## What a visitor sees

- **Reactions and votes** count straight away.
- **RSVPs** ask for a name the first time, so whoever organizes the event knows
  who's coming. In the list of answers a visitor shows as "Robin (visitor)", and a
  visitor can't use a name a member already has.
- After they act, the tag says **Sign in to keep this**. Style or hide that line
  with `::part(visitor)`.

## Signing in keeps it

The visitor's browser remembers what they did for a year. When they sign in:

- **With a new email,** they become a member and everything they did is theirs.
- **With an email that already has an account,** what they did moves onto that
  account. Where both did the same thing, like voting in the same poll, the
  account's own answer is kept.

If they clear their browser's cookies first, what they did stays counted but
can't be carried over.

## Keeping it fair

- Visitors can only react, vote and RSVP. Commenting, posting, following, chat
  and groups still need a member.
- A visitor gets one vote per browser, so someone determined can vote twice with
  a second browser. Keep polls that matter for members.
- friendo limits how many new visitors, and how many clicks, come from one
  network address in a short time.
- Visitors don't appear in admin **Users** and have no profile page.
