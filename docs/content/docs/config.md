---
title: friendo.toml
slug: config
section: Reference
weight: 20
description: Every key in friendo.toml.
---

`friendo.toml` sits at the root of your site and configures it. A minimal file:

```toml
[site]
name = "My Site"
timezone = "America/Los_Angeles"

[content]
collections = ["blog", "events", "pages"]
```

## `[site]`

| Key | What |
|---|---|
| `name` | The site's name, `{{ site.name }}` in templates |
| `timezone` | The zone an [event's](/docs/calendar) `when` is read in, as an IANA name (`America/Los_Angeles`, `Europe/Paris`). `friendo init` fills in your machine's. Unset means UTC |

## `[content]`

| Key | What |
|---|---|
| `collections` | The site's [collections](/docs/posts#collections), in the order the admin lists them |

```toml
[content]
collections = ["blog", "events", "pages"]
```

A collection exists simply by having posts in it (via the admin, the API, or a
`content/` folder), so you can start one ad hoc and let the file catch up: the
admin marks it ✱ until you add it here. **Settings → Content** shows the
`[content]` block that matches the site right now, every collection and the fields
its posts carry, to paste into your file; `friendo pull` writes that same block
into your local `friendo.toml` for you (only the `[content]` section).

Leave the key out and the admin shows the built-in defaults (`blog`, `pages`) plus
whatever has posts.

### Fields

Optionally, say which fields a collection's posts carry. The admin then shows those
fields on every post (even before a post has them), uses them as the table's
columns, and checks `required` and `choices` before saving. Because it's in
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
`required`, and a `hint` shown under the field. A field can't be named after one
of a post's own parts (`title`, `slug`, `body`, `status`, …) or `when`, which the
[When section](/docs/calendar) owns.

This describes the fields; it doesn't restrict them. A post can still carry any
other key, from a content file, a `<friendo-form>` or the admin's *Add a field*,
and the admin shows those after the declared ones.

## `[profiles]`

The fields a member's [profile](/docs/profiles) carries beyond its
name, avatar and bio. Same grammar as a collection's fields; the profile editor,
`<friendo-profile>` and the admin lay themselves out from it, and the values are
`profile.fields.<name>` on `pages/profiles/[slug].html`:

```toml
[profiles.fields]
pronouns = "text"
website  = { kind = "text", hint = "https://…" }
```

A field can't be named after one of a profile's own parts (`name`, `slug`,
`avatar`, `bio`, `email`, …).

## `[settings]`

Every setting in admin **Settings** can be declared here instead, handy for
keeping a site's policy in version control and shipping it with the folder. Each
setting has one name: the same word here, in the admin and in the API.
**`friendo.toml` is the source of truth for any key you set here:** it's applied
on startup (overwriting the stored value) and that control is shown **read-only**
in the admin. Omit a key and it stays editable in the admin as usual.

```toml
[settings]
open_signups = true                # anyone can sign up (off: only people an admin adds)
signups_are_contributors = false   # a new member starts as a contributor
members_can_post = false           # members can post from a <friendo-form>; their posts wait for review
posts_need_review = false          # contributors' posts wait for review before they go live
comments_need_review = true        # comments wait for review before they show
password_login = false             # also allow signing in with a password
profile_visibility = "members"     # who sees profiles: "members" or "public"
members_can_start_groups = false   # any member can start a group (and is its admin)
visitors_can_react = false         # someone who hasn't signed in can react
visitors_can_vote = false          # ... vote in polls
visitors_can_rsvp = false          # ... answer an event, giving a name
comments = true                    # feature switches, see below
default_collections = ["blog", "pages"]   # built-in collections shown while [content] collections is unset
```

| Key | What | Default |
|---|---|---|
| `open_signups` | Anyone can sign up. Off, only people an admin adds have accounts | `true` |
| `signups_are_contributors` | A new member starts as a contributor (can write their own posts). Off, a new member can comment, react, vote and RSVP | `false` |
| `members_can_post` | Members can post from a [`<friendo-form>`](/docs/forms); their posts always wait for review | `false` |
| `posts_need_review` | A contributor's post waits for review until a moderator approves it | `false` |
| `comments_need_review` | A new comment waits for review until a moderator approves it | `true` |
| `password_login` | Also allow signing in with a password. Everyone can always sign in with an emailed code. See [Signing in](/docs/signing-in) | `false` |
| `profile_visibility` | Who may see [profiles](/docs/profiles): `members` (anyone signed in) or `public` | `members` |
| `members_can_start_groups` | Any member can start a [group](/docs/groups) and is its admin. Off, contributors and up can | `false` |
| `visitors_can_react`, `visitors_can_vote`, `visitors_can_rsvp` | Someone who hasn't signed in can react, vote or RSVP. Their browser remembers them, and signing in brings what they did along. See [Let visitors take part](/docs/visitors) | `false` |
| `comments`, `reactions`, `polls`, `rsvp`, `locations`, `chats`, `follows`, `groups` | Feature switches (admin **Settings → Features**). Off, the feature's API refuses it, its `<friendo-*>` tag renders nothing, and `post.comments` (and so on) is empty. What people already wrote is kept | `true` |
| `default_collections` | Which built-in collections (`blog`, `pages`) the admin lists while `[content] collections` is unset | both |

Changes take effect on the next start (like `[site].name`). Keys you leave out are
managed in the admin and travel to a deployed site with `friendo push`.

## `[access]`

Paths that are for members only, so a whole folder can be
[members-only](/docs/members-only) without a tag on each page:

```toml
[access]
members_only = ["/members/*", "/downloads"]   # anyone signed in
editors_only = ["/newsroom/*"]                # editors and up
```

Also `contributors_only`, `moderators_only`, `admins_only` and `owners_only`. A
path can be kept for one [group](/docs/groups)'s members:

```toml
[access]
groups = { "/board/*" = "board" }
```

A pattern ending in `/*` covers that path and everything under it (a page that
doesn't exist there shows the sign-in page rather than a 404, so the folder
doesn't reveal what's inside); any other pattern must match the whole path. A page
under a kept path can still ask for more with its own tag. Takes effect on the
next start.

## `[deploy]`

Written by `friendo deploy`, but you can set it yourself:

| Key | What |
|---|---|
| `target` | The deployed site's URL (`https://my-site.friendo.world`). `push` and `pull` use it by default |
| `domain` | An optional custom domain to show as the site's live URL |

```toml
[deploy]
target = "https://my-site.friendo.world"
# domain = "mysite.com"
```

## The rest of the folder

Everything else about a site is convention, not config: `layouts/`, `pages/`,
`content/`, `assets/` and `data/`. See [A site is a folder](/docs/site-is-a-folder).
