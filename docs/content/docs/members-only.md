---
title: Make members-only pages
slug: members-only
section: Guides
topic: Community
weight: 10
description: Keep a page, a folder or a group's corner of the site for the people it's meant for, with one tag or a line in friendo.toml.
---

To keep a page for signed-in members, put `{% members only %}` at the top of its
template. A visitor who opens it gets a sign-in page at the same URL, and the real
page once they sign in. The steps below cover one page, a whole folder, a group,
and the sign-in page people see instead.

## 1. Put the tag on the page

```html
{# pages/members.html #}
{% extends "layouts/base.html" %}
{% members only %}
{% block content %}<h1>Welcome back, {{ user.name }}</h1>{% endblock %}
```

Open `/members` without signing in and you see a small built-in sign-in page: the
site's name, a line about why, and a `<friendo-signin>` box. Sign in with the code
and the page reloads with your name in it.

The tag works at the top of a page, inside a block, or in a layout. Put
`{% members only %}` in `layouts/members.html` and every page that extends it is
members-only.

## 2. Pick who gets in

Swap the first word for the lowest role that may see the page. Each one lets in
that role and every role above it:

| Tag | Who gets in |
|---|---|
| `{% members only %}` | anyone signed in |
| `{% contributors only %}` | contributors and up |
| `{% moderators only %}` | moderators and up |
| `{% editors only %}` | editors and up |
| `{% admins only %}` | admins and owners |
| `{% owners only %}` | owners |
| `{% members only if user.email == "pat@example.com" %}` | signed in **and** the condition holds |
| `{% members only if "board" in user.groups %}` | signed in **and** in the [group](/docs/groups) `board` |

The part after `if` is an ordinary template expression. It can read `user`, and on
a post's page `post`. The roles are listed in [Roles](/docs/roles).

## 3. Keep a whole folder

To keep many pages without touching each one, list their paths under `[access]` in
[friendo.toml](/docs/config):

```toml
[access]
members_only = ["/members/*", "/downloads"]   # anyone signed in
editors_only = ["/newsroom/*"]                # editors and up
```

There's a key for each role: `members_only`, `contributors_only`,
`moderators_only`, `editors_only`, `admins_only` and `owners_only`. A pattern
ending in `/*` covers that path and everything under it; any other pattern must
match the whole path. Under a `/*` pattern, a page that doesn't exist shows the
sign-in page rather than a 404, so the folder doesn't reveal what's inside.

A page under a kept path can still ask for more with its own tag. Restart
`friendo serve` after changing `[access]`; it's read on startup.

## 4. Keep a page for one group

`user.groups` is the list of group slugs the viewer belongs to, so a page can ask
for one:

```html
{% members only if "board" in user.groups %}
```

Or keep a whole folder for a group's members in friendo.toml:

```toml
[access]
groups = { "/board/*" = "board" }
```

`user.groups` holds only the groups the member has joined. A site admin who isn't
in `board` can run the group from the admin, but these pages turn them away until
they join it.

## 5. Write your own sign-in page

Add `pages/login.html` and friendo shows it in place of the built-in one. It gets a
`gate` variable so it can explain itself:

- `gate.reason`: `signin` (nobody is signed in), `role` (signed in, but the role is
  too low) or `condition` (the `if` part was false, or they're not in the group).
- `gate.required`: the lowest role the page asked for, such as `editor`.
- `gate.path`: the URL they tried to open.

```html
{# pages/login.html #}
{% extends "layouts/base.html" %}
{% block content %}
  {% if gate.reason == "signin" %}<p>Sign in to see {{ gate.path }}.</p>
  {% elif gate.reason == "role" %}<p>That page is for {{ gate.required }}s and up.</p>
  {% else %}<p>That page isn't available to your account.</p>{% endif %}
  <friendo-signin reload></friendo-signin>
{% endblock %}
```

The response is status 401 when nobody is signed in and 403 when someone is but
isn't let in. If `login.html` itself fails to render, friendo falls back to the
built-in page.

## 6. Add `reload` to the sign-in box

`<friendo-signin>` signs people in without leaving the page. Anything the server
rendered from `{{ user }}`, including whether the page was let through at all, is
still the old version until the page loads again. `reload` does that for you after
signing in or out:

```html
<friendo-signin reload></friendo-signin>
```

Use it on your `login.html` and in any layout whose pages show `{{ user }}`.
`friendo init` already puts it in the scaffolded `layouts/base.html`.

## 7. Show different things to members and visitors

Not every page needs a gate. `user` is the signed-in member, or empty for a visitor,
so one page can greet both:

```html
{% if user %}Hi {{ user.name }}{% else %}<friendo-signin reload></friendo-signin>{% endif %}
```

`user` also carries `role`, `groups` and more; the full list is in
[Template variables](/docs/template-variables).

> **Common mistake:** gating the page but not the list. `{% members only %}` on
> `pages/members/[slug].html` keeps each post's page, but a public page that loops
> over `collections.members` still prints every title and link. Put the listing
> page under the same tag or `[access]` path, or wrap the loop in `{% if user %}`.

> **Note:** members-only pages are left out of a
> [static export](/docs/static-export), and the export lists each one it skips. A
> static host can't check who's asking.

**Next:** [Add profiles and follows](/docs/profiles), or see how
[signing in](/docs/signing-in) and [roles](/docs/roles) work.
