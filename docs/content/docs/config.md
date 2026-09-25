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
timezone = "America/Los_Angeles"

[content]
types = ["posts", "comments", "reactions"]
```

## `[site]`

| Key | What |
|---|---|
| `name` | The site's display name, available in templates as `{{ site.name }}` |
| `timezone` | The zone an [event's](/docs/calendar) `when` is read in, as an IANA name (`America/Los_Angeles`, `Europe/Paris`). `friendo init` fills in your machine's. Unset means UTC |

## `[content]`

| Key | What |
|---|---|
| `types` | The site's [content types](/docs/content) — the collections the admin lists, in this order |

```toml
[content]
types = ["blog", "events", "pages"]
```

The admin lists these first, in this order, then any other collection that has
records — a collection exists simply by having records in it (via the admin, the
API, or a `content/` folder), so you can start one ad hoc and let the file catch up.
Leave the key out and the admin shows the built-in defaults (`blog`, `pages`,
`posts`) plus whatever has records.

**Keeping the file in step.** A collection that isn't in `types` yet is marked ✱ in
the admin. The **friendo.toml** button on any collection shows the `[content]` block
that matches the site right now — every collection and the fields its records carry —
to paste into your file. `friendo pull` writes that same block into your local
`friendo.toml` for you (only the `[content]` section; everything else is left alone).

### Fields

Optionally, say which fields a type's records carry. The admin then shows those
fields on every record of the type (even before a record has them), uses them as the
table's columns, and checks `required` and `choices` before saving. Because it's in
`friendo.toml`, it travels with the folder to every install of the site.

```toml
[content.blog.fields]
tags   = "tags"
cover  = "image"
weight = "number"
mood   = { kind = "text", choices = ["calm", "wild"], required = true, hint = "How the post feels" }
```

A bare string is the field's **kind**: `text`, `paragraph`, `number`, `checkbox`,
`tags`, `image` or `json`. A table adds `choices` (a menu instead of free text),
`required`, and a `hint` shown under the field. Field names can't be one of a
record's own fields (`title`, `slug`, `body`, `status`, …) or the calendar keys
(`when`, `ends`, `repeats`, …), which the [When section](/docs/calendar) owns.

This describes the fields; it doesn't restrict them. A record can still carry any
other key — from a `content/` file's front matter, a `<friendo-form>`, or the
admin's *Add a field* — and the admin shows those after the declared ones. The
records API accepts any data either way.

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
comments = true                 # feature switches: comments, reactions, polls, rsvp, locations, channels
default_collections = ["blog", "pages"]   # built-in collections shown when [content] types is unset
```

| Key | What | Default |
|---|---|---|
| `default_role` | Role a self-serve visitor gets on sign-up — `member` or `contributor` | `member` |
| `signups_enabled` | Whether the public can create accounts | `true` |
| `require_approval` | Hold contributor posts as drafts until an editor publishes them | `false` |
| `accept_submissions` | Let signed-in members submit posts from a [`<friendo-form>`](/docs/community#submitting-posts-from-a-page) into the review queue | `false` |
| `auto_approve` | Publish new comments immediately instead of queuing them for moderation | `false` |
| `password_login` | Also allow signing in with a password. Off, everyone signs in with a code sent to their email — see [Auth & users](/docs/auth) | `false` |
| `comments`, `reactions`, `polls`, `rsvp`, `locations`, `channels` | Feature switches (admin **Settings → Features**). Off, the feature's API refuses reads and writes with `403` and `off: true`, its `<friendo-*>` tag renders nothing, and `record.comments` (and so on) is empty in templates. What people already wrote is kept | `true` |
| `default_collections` | Which of the built-in collections (`blog`, `pages`, `posts`) the admin lists while `[content] types` is unset | all three |

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
