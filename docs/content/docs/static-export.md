---
title: Static export
slug: static-export
group: Reference
weight: 22
---

Don't want a server at all? Render the whole site to plain HTML and put it on
any static host — GitHub Pages, Netlify, an S3 bucket, a folder on a USB stick.

```bash
friendo export --mode static
```

That writes `dist/` next to your site: every page in `pages/` rendered with your
content, every published record's page, plus `assets/`. Only **published**
records are included; drafts and pending posts stay out, the same as on the
live site. [Members-only pages](/docs/templates#members-only-pages) are skipped
too (the export lists each one), since a static host can't tell who's asking.

Community features that need the runtime — signing in, posting comments, live
channels — don't work in a static export, since there's no API behind it. The
**server-rendered** forms still do: `{{ record.comments }}`, `{{ record.reactions }}`,
`{{ record.poll }}` and `{{ record.gallery }}` are rendered into the HTML at
export time, so readers get the content even without the runtime.

## Bundle

```bash
friendo export --mode bundle
```

A **bundle** is the site folder packed up to move somewhere: templates, assets,
and the database, with `data/` scrubbed of sessions and one-time codes. It's
what you'd hand to another friendo runtime, not to a static host.

`export` compiles `content/` first (like `serve` and `push` do), so the output
always reflects your latest markdown.
