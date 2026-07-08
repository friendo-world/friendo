---
title: friendo.toml
slug: config
group: Reference
weight: 9
---

`friendo.toml` sits at the root of your site and configures it. A minimal file:

```toml
[site]
name = "My Site"

[content]
types = ["posts", "comments", "reactions"]
```

## `[site]`

| Key | What |
|---|---|
| `name` | The site's display name, available in templates as `{{ site.name }}` |

## `[content]`

| Key | What |
|---|---|
| `types` | The [content types](/docs/content) your site uses — a hint for tooling |

`types` documents which collections your site works with. Collections themselves
are created simply by having records in them (via the admin UI, the API, or a
`content/` folder), so this is descriptive rather than enforced.

## `[deploy]`

Written by `friendo deploy`, but you can set it yourself:

| Key | What |
|---|---|
| `target` | The deployed site's URL (e.g. `https://my-site.friendo.world`). `push`/`pull` use it by default |
| `domain` | An optional custom domain to show as the site's live URL |

```toml
[deploy]
target = "https://my-site.friendo.world"
# domain = "mysite.com"
```

## The rest of the folder

Everything else about a site is convention, not config — `layouts/`, `pages/`,
`assets/`, and `data/`. See [A site is a folder](/docs/site-is-a-folder).
