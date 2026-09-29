---
title: Add profiles and follows
slug: profiles
section: Guides
topic: Community
weight: 20
description: Give every member a page, let people follow each other, show a feed of posts from people they follow, and tell them what happened in an inbox.
---

To give your members a public face, add a profile page. With it come a follow
button, a feed of posts from the people someone follows, and an inbox that tells a
member when something happened. `friendo init` scaffolds the profile page and the
inbox; the steps below show what's in them and how to change them.

Every member already has a **profile**: a name, an avatar, a short bio and a slug.
It's made with the account and named after it, so Pat becomes `pat` (a second Pat
is `pat-2`). Why an account and a profile are two things is in
[The social graph](/docs/social-graph).

## 1. Add the profile page

Make `pages/profiles/[slug].html`. Every profile now has a page at
`/profiles/<slug>`, and on it `profile` is the profile:

```html
{# pages/profiles/[slug].html #}
{% extends "layouts/base.html" %}
{% block content %}
  <h1>{{ profile.name }}</h1>
  {% if profile.bio %}<p>{{ profile.bio }}</p>{% endif %}
  <friendo-profile slug="{{ profile.slug }}" edit-only></friendo-profile>

  <h2>Posts</h2>
  <ul>
  {% for p in profile.posts %}
    <li><a href="/{{ p.collection }}/{{ p.slug }}">{{ p.title }}</a></li>
  {% empty %}
    <li>Nothing yet.</li>
  {% endfor %}
  </ul>
{% endblock %}
```

`profile` carries `name`, `slug`, `avatar`, `bio`, `url`, `fields`, and beside
them `profile.posts` (their published posts), `profile.comments` (their approved
comments), `profile.followers` and `profile.following`. A profile never includes the
email. `collections.profiles` lists every profile, for a page of everyone.

The folder name `profiles` is taken for this page, so a collection can't be called
`profiles`. On any post, `post.author` is the writer's profile, so link to it with
`/profiles/{{ post.author.slug }}`.

## 2. Let people edit their own

`<friendo-profile slug="…" edit-only>` shows nothing to other people. On your own
profile it shows an **Edit profile** button; saving reloads the page so the
server-rendered name and bio catch up. Without `edit-only` the tag draws the whole
profile itself.

Members can also edit from the *profiles* switcher in `<friendo-signin>`, which is
where someone with more than one profile picks which one their posts, comments and
messages are by.

## 3. Add fields to profiles

Declare extra fields once in friendo.toml, with the same grammar as a collection's
[fields](/docs/config):

```toml
[profiles.fields]
pronouns = "text"
website  = { kind = "text", hint = "https://…" }
```

The profile editor and `<friendo-profile>` lay themselves out from it, and the
values read back as `{{ profile.fields.pronouns }}`. A field can't use the name of
one of a profile's own parts (`name`, `slug`, `avatar`, `bio`, `email`, …).

## 4. Decide who sees profiles

`profile_visibility` is `members` by default. A visitor who opens a profile page
then gets the sign-in page, exactly like a [members-only page](/docs/members-only),
and `collections.profiles` is empty for them. Set it to `public` and everyone can
see profiles.

Change it in the admin under **Settings → Members & roles** (*Profiles are visible
to*), or in friendo.toml:

```toml
[settings]
profile_visibility = "public"
```

The Community and Blog presets set it to `public`; Personal keeps `members`.

## 5. Add the follow button

Following is one way, like a subscription. Two people who follow each other are
**friends**; friendo works that out, and there's nothing to accept. Put the button
on the profile page:

```html
<friendo-follow profile-id="{{ profile.id }}"></friendo-follow>
```

It shows **Follow** (or **Following**, pressed) and the follower count. On your own
profile it shows only the count. A visitor who clicks it fires
[`friendo:needs-auth`](/docs/sdk-events), so your page can point them at the sign-in box.
`slug="pat"` works in place of `profile-id`.

The counts render without JavaScript too:

```html
<small>{{ profile.follower_count }} followers · {{ profile.following_count }} following</small>
```

## 6. Show a feed of followed posts

The viewer carries their side of the graph: `user.following`, `user.followers` and
`user.friends`, each a list of profile ids. The `by_following` filter turns it into
a feed:

```html
{% if user %}
  <h2>From people you follow</h2>
  {% for p in collections.blog|by_following:user %}
    <a href="/blog/{{ p.slug }}">{{ p.title }}</a> by {{ p.author.name }}
  {% empty %}
    <p>Follow someone and their posts land here.</p>
  {% endfor %}
{% endif %}
```

The page renders `{{ user }}` on the server, so give its `<friendo-signin>` the
`reload` attribute; otherwise the feed stays empty until the next page load.

## 7. Add the inbox page

The **inbox** is where a member sees what happened while they were away. Friendo
writes a note to it when:

- someone follows them,
- someone comments on their post (once the comment is approved),
- someone asks to join a group they moderate,
- they're added to a group, or their request to join is approved,
- someone invites them to an event.

Nothing is emailed; the sign-in code is the only email friendo sends. Put
`<friendo-inbox>` on a members-only page:

```html
{# pages/inbox.html #}
{% extends "layouts/base.html" %}
{% members only %}
{% block content %}
<h1>Inbox</h1>
<friendo-inbox></friendo-inbox>
{% endblock %}
```

It lists the notes newest first, unread in bold, each with *mark read*, and a
*Mark all read* at the top. `<friendo-signin>` shows an unread badge by itself. For
a badge of your own, use `{{ user.unread }}`:

```html
{% if user %}<a href="/inbox">Inbox{% if user.unread %} ({{ user.unread }}){% endif %}</a>{% endif %}
```

## 8. Check the switches

Follows are the **Follows** switch in **Settings → Features** (`follows` under
`[settings]` in friendo.toml). Off, `<friendo-follow>` renders nothing, the follow
lists are empty, and `by_following` finds no posts. The inbox is on while
**Follows** or **Groups** is on. Profiles have no switch; `profile_visibility` is
the lever.

> **Common mistake:** testing follows while signed in only to the admin. On
> `localhost` the admin opens without a sign-in, but your pages still treat you as a
> visitor, and the admin's owner has no profile to follow from. Sign in on the page
> with `<friendo-signin>` (the code shows in the form) to follow, comment or read
> an inbox.

Profiles and follows travel to a deployed site with `friendo push --users`; notes
in the inbox stay with the site they were written on. See
[Push and pull](/docs/push-and-pull).

**Next:** [Groups](/docs/groups), or the idea behind all of this in
[The social graph](/docs/social-graph).
