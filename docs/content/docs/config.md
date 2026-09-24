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

## `[settings]`

The behavioural settings you'd otherwise flip in admin **Settings** can be declared
here instead — handy for keeping a site's policy in version control and shipping it
with the folder. **`friendo.toml` is the source of truth for any key you set here:**
it's applied on startup (overwriting the stored value) and that control is shown
**read-only** in the admin UI. Omit a key and it stays editable in the admin UI as
usual.

```toml
[settings]
default_role = "member"        # what a new sign-up becomes: "member" or "contributor"
signups_enabled = true         # allow public sign-ups
require_approval = false        # hold contributor posts for review before publishing
accept_submissions = false      # let members submit posts via <friendo-form> (into the review queue)
auto_approve = false            # publish new comments immediately instead of queuing them
password_login = false          # also allow signing in with a password (everyone can always use an emailed code)
```

| Key | What | Default |
|---|---|---|
| `default_role` | Role a self-serve visitor gets on sign-up — `member` or `contributor` | `member` |
| `signups_enabled` | Whether the public can create accounts | `true` |
| `require_approval` | Hold contributor posts as drafts until an editor publishes them | `false` |
| `accept_submissions` | Let signed-in members submit posts from a [`<friendo-form>`](/docs/community#submitting-posts-from-a-page) into the review queue | `false` |
| `auto_approve` | Publish new comments immediately instead of queuing them for moderation | `false` |
| `password_login` | Also allow signing in with a password. Off, everyone signs in with a code sent to their email — see [Auth & users](/docs/auth) | `false` |

Changes take effect on the next start (like `[site].name`). Keys you leave out are
managed in the admin UI and travel to a deployed site with `friendo push --data`.

## `[access]`

Paths that are for signed-in people only, so a whole folder can be
[members-only](/docs/templates#members-only-pages) without a tag on each page:

```toml
[access]
members_only = ["/members/*", "/downloads"]   # anyone signed in
editors_only = ["/newsroom/*"]                # editors and up
```

Also `contributors_only`, `admins_only` and `owners_only`. A pattern ending in `/*`
covers that path and everything under it (a page that doesn't exist there shows
the sign-in page rather than a 404, so the folder doesn't reveal what's inside);
any other pattern must match the whole path. A page under a gated path can still
ask for more with its own tag. Takes effect on the next start.

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
