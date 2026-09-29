---
title: Static export
slug: static-export
section: Reference
weight: 22
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
site. [Members-only pages](/docs/templates#members-only-pages) are skipped too
(the export lists each one), since a static host can't tell who's asking.

Community features that need the runtime (signing in, writing comments, live
chats) don't work in a static export, since there's no API behind it. The
**server-rendered** spellings still do: `post.comments`, `post.reactions`,
`post.poll` and `post.gallery` are rendered into the HTML at export time, so
readers get the content even without the runtime.

The social pieces follow the same rule. [Profile pages](/docs/signing-in#profiles)
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
