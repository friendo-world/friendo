---
title: Export a static site
slug: static-export
section: Guides
topic: Hosting
weight: 50
description: Write the site as plain HTML for any static host.
---

Don't want a server at all? Render the whole site to plain HTML and put it on
any static host: GitHub Pages, Netlify, an S3 bucket, a folder on a USB stick.

```bash
friendo export --mode static
```

That writes `dist/` next to your site: every page in `pages/` rendered with your
posts, every published post's page, `assets/`, and, if any post has a `when`, the
[calendar feeds](/docs/calendar#subscribe-calendarics) `calendar.ics` and
`calendar.json`, so a static site is subscribable. Only **published** posts are
included; drafts and posts waiting for review stay out, the same as on the live
site. [Members-only pages](/docs/members-only) are skipped too
(the export lists each one), since a static host can't tell who's asking.

Community features need the runtime, so they don't work in a static export:
there's no API behind it for signing in, writing comments or live chats. The
community's server-rendered lists are empty too. `post.comments`,
`post.reactions`, `post.poll`, `post.gallery` and `post.rsvps` are attached by the
live server, not by the export, so a template that renders them prints nothing.

> **Common mistake:** a page that lists comments or reaction tallies with
> `{% for c in post.comments %}` looks right under `friendo serve` and empty in
> `dist/`. Give that part a `{% empty %}` line, or keep a site with a community
> on a server. See [Server-rendered and live](/docs/two-spellings).

What an export does carry: each post's `when`, `location`, `author` and `group`.
Profiles and groups follow what a visitor may see. [Profile pages](/docs/profiles)
are exported only when `profile_visibility` is `public`; a members-only site
exports none. A [group](/docs/groups) that isn't public, and every post filed
under it, is left out, as it is for a visitor. `post.author`, `group.members` and
the follower counts are baked in; `<friendo-follow>`, `<friendo-group>`,
`<friendo-inbox>` and the rest need the runtime.

## Bundle

```bash
friendo export --mode bundle
```

A **bundle** is the site folder packed up to move somewhere: templates, assets,
and the database, with `data/` scrubbed of sessions and one-time codes. It's what
you'd hand to another friendo runtime, not to a static host.

`export` reads `content/` first (like `serve` and `push` do), so the output always
reflects your latest markdown.
