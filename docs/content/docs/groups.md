---
title: Start groups
slug: groups
section: Guides
topic: Community
weight: 30
description: A group is a post with members. Make one, set who may join, and file posts under it.
---

A **group** is a post with members, the way an [event](/docs/calendar) is a post
with a `when`. It lives in the `groups` collection like any other post: it has a
title, a slug, a body and a page, and it can be made from the admin, a
`content/groups/*.md` file, or a form on a page. What makes it a group is the
people friendo keeps for it, and two settings in its fields:

```yaml
---
title: The Board
slug: board
visibility: private     # public | members | private
join: request           # open | request | invite
---
Where the board keeps its notes.
```

| Setting | Values |
|---|---|
| `visibility` | `public`: everyone sees the group and what's filed under it (the default) · `members`: anyone signed in · `private`: only its members, and nobody else can tell it exists |
| `join` | `open`: join at once (the default) · `request`: ask, and a moderator approves · `invite`: admins add people |

## Admins, moderators, members

A group mirrors the site. Whoever makes a group is its first **admin**: admins
change the two settings, add and remove people, make other admins and moderators,
and manage the group's chats. **Moderators** keep it tidy: they approve requests to
join, remove members and delete messages. Everyone else is a **member**.

Site admins are admins of every group, and site moderators (and up) moderate every
group. That's also how a group written as a `content/groups/*.md` file gets going:
a file has no author, so the group starts with no admin until a site admin makes
one, from the group's **Members** section in the admin or from `<friendo-group>`.

A group always keeps at least one admin; the last one can't leave.

## The pages

`friendo init` scaffolds `pages/groups/index.html` and `pages/groups/[slug].html`.
On a group's page the group is `group`: `group.settings` (`visibility`, `join`),
`group.members` (profiles, admins and moderators first, each with a `role`),
`group.admins`, `group.moderators`, `group.member_count` and `group.chats`.
Everything a viewer can't see is already gone from `collections.groups`.

```html
{# pages/groups/[slug].html #}
<h1>{{ group.title }}</h1>
<p>{{ group.member_count }} members · join: {{ group.settings.join }}</p>
<friendo-group post-id="{{ group.id }}"></friendo-group>
```

`<friendo-group>` is the live side: the member count, a *Join* / *Ask to join* /
*Leave* button that follows the join rule and the viewer's standing, the member
list, and the levers: requests to approve, make admin / make moderator / demote,
remove, add by profile name, the group's chats and its settings, each shown to
whoever may use it. `<friendo-groups>` on the index lists the groups the viewer may
see, with a *start a group* form when they may.

## Who can start a group

Contributors and up always can, from the admin or `<friendo-groups>`. Turn on
**Members can start groups** in **Settings → Members & roles**
(`members_can_start_groups = true` in friendo.toml) and any member can start one
and is its admin.

## Filing things under a group

A post or event belongs to a group by naming it: `group: board` in its front
matter, or a `group` field in a form. Nothing else to set up:

```html
{% for p in collections.blog|in_group:"board" %}…{% endfor %}
{% for e in collections.events|in_group:group.slug|upcoming %}…{% endfor %}
```

`post.group` on any post is its group, or empty. A private group's posts and
events are hidden from everyone who isn't in it: in `collections.*`, on their own
pages, and in the public calendar feed. `/calendar.ics?group=board` is a group's
own feed, public for a public group; for the others, members subscribe through
their [private calendar link](/docs/calendar#your-private-calendar). A group's
chats are its members' only: see [Group chats](/docs/chats#group-chats).

The value is a plain string compare, so a `group:` key that names no group is
just a field.

## Pages for a group's members

Templates see `user.groups`, the slugs the viewer belongs to, so the
[members-only tag](/docs/members-only) takes it as a condition:

```html
{% members only if "board" in user.groups %}
```

Or keep a whole folder for a group in `friendo.toml`:

```toml
[access]
groups = { "/board/*" = "board" }
```

Memberships travel with `friendo push --posts`. The whole feature is the
**Groups** switch in **Settings → Features**; joining, requests and additions
write to the [inbox](/docs/profiles). The routes are in the
[API](/docs/api#groups) reference.
