---
title: Templates
slug: templates
section: Concepts
weight: 6
---

Friendo templates are [Pongo2](https://github.com/flosch/pongo2), a
Jinja2-compatible language, in plain `.html` files. One runtime renders them
everywhere, so a template looks the same on your laptop, on a server you run, and
on friendo.world.

## Layouts and blocks

Put shared chrome in `layouts/` and extend it from pages:

```html
{# layouts/base.html #}
<!DOCTYPE html>
<html>
  <head><title>{% block title %}{{ site.name }}{% endblock %}</title></head>
  <body>{% block content %}{% endblock %}</body>
</html>
```

```html
{# pages/index.html #}
{% extends "layouts/base.html" %}
{% block content %}<h1>Hello</h1>{% endblock %}
```

## Pages and URLs

Every file in `pages/` maps to a URL by its path: `pages/about.html` is `/about`,
`pages/blog/index.html` is `/blog`.

### One page per post

A path segment in square brackets is a parameter, and the **folder name is the
collection** it looks up. `pages/blog/[slug].html` serves `/blog/<slug>` and finds
the post in the `blog` collection with that slug. On that page, the post is `post`:

```html
{# pages/blog/[slug].html #}
{% extends "layouts/base.html" %}
{% block content %}
  <h1>{{ post.title }}</h1>
  <div>{{ post.body|markdown }}</div>
{% endblock %}
```

Two kinds of post also answer to their own name, so a template reads naturally:
on a group's page `group` is the group (`{{ group.members }}`), and on any post with
a time `event` is the event (`{{ event.when }}`). `post` always works too; they're
the same thing.

One folder is special: `pages/profiles/[slug].html` renders a member's
[profile](/docs/signing-in#profiles), not a post. There the profile is `profile`.

## What every page sees

| Variable | What |
|---|---|
| `site.name` | Your site's name from friendo.toml |
| `site.url` | The site's own address as the request saw it (`https://my-site.friendo.world`); in a static export, from `[deploy]` |
| `request.path` | The current URL path |
| `collections.<name>` | Every published post in a collection, each with its `when`, `author` and `group` |
| `collections.profiles` | Every member's profile (empty for a visitor while profiles are members-only) |
| `user` | Who's signed in, or empty for a visitor. See [below](#members-only-pages) |
| `calendar.google` / `.webcal` / `.ics` | [Subscribe links](/docs/calendar#google-calendar-apple-calendar-outlook) for the site's events |
| `post` | On a post's page: the post. Also `group` on a group's page and `event` on an event's |
| `profile` | On a profile page: the profile |
| `gate` | Only on your `login.html` when it stands in for a members-only page: `required`, `reason`, `path` |

### What a post carries

| On `post` | What |
|---|---|
| `id`, `slug`, `title`, `body`, `status`, `published_at`, `created`, `updated` | The post's own parts |
| `fields.<name>` | Its [fields](/docs/posts#fields): `{{ post.fields.mood }}` |
| `author` | The writer's [profile](/docs/signing-in#profiles): `name`, `slug`, `avatar`, `bio`, `url`, `fields` |
| `when` | Its time, if it's an [event](/docs/calendar): `start`, `end`, `all_day`, `timezone`, `repeats`, `next`; printed on its own it reads "Sat Oct 4, 10 am – 4 pm" |
| `location` | Its [place](/docs/locations), if any: `lat`, `lng`, `label` |
| `group` | The [group](/docs/groups) it's filed under, or empty |
| `comments` | The **approved** comments, oldest first: `author_name`, `body`, `created` |
| `reactions` | Reaction tallies: `emoji`, `count` |
| `poll` | Its poll, if the front matter declares one: `question`, `options` with `text` and `votes` |
| `gallery` | Images from the post's [folder](/docs/posts#a-post-as-a-folder), each with a `url` |
| `rsvps` | An event's [RSVP](/docs/calendar#rsvp) tally for its next date: `going`, `maybe`, `not_going`, `invited` |
| `members`, `admins`, `moderators`, `member_count`, `settings`, `chats` | On a group |

Everything the community adds is attached before the template runs, so it renders
with no JavaScript:

```html
<ul>{% for r in post.reactions %}<li>{{ r.emoji }} {{ r.count }}</li>{% endfor %}</ul>
<ol>{% for c in post.comments %}<li><b>{{ c.author_name }}</b>: {{ c.body }}</li>{% endfor %}</ol>
{% for img in post.gallery %}<img src="{{ img.url }}" alt="">{% endfor %}
```

The [`<friendo-*>` tags](/docs/community) are the interactive spellings of the same
things: `post.comments` for readers and search engines, `<friendo-comments>` for
signing in and writing one.

### What `user` carries

`name`, `email`, `role`, `id`, `profile_id` (their current profile), `following`,
`followers` and `friends` (profile ids), `groups` (the slugs of the groups they're
in) and `unread` (notifications waiting in their [inbox](/docs/community#notifications)).

## Members-only pages

`user` is the signed-in member, or empty for a visitor:

```html
{% if user %}Hi {{ user.name }}{% else %}<friendo-signin reload></friendo-signin>{% endif %}
```

To make a whole page for members, put one tag at the top:

```html
{# pages/members.html #}
{% extends "layouts/base.html" %}
{% members only %}
{% block content %}<h1>Welcome back, {{ user.name }}</h1>{% endblock %}
```

| Tag | Who gets in |
|---|---|
| `{% members only %}` | anyone signed in |
| `{% contributors only %}` | contributors and up |
| `{% moderators only %}` | moderators and up |
| `{% editors only %}` | editors and up |
| `{% admins only %}` | admins and owners |
| `{% owners only %}` | owners |
| `{% members only if user.email == "pat@example.com" %}` | signed in **and** the condition holds; any expression over `user`, `post`, … |
| `{% members only if "board" in user.groups %}` | signed in **and** in the [group](/docs/groups) `board` |

The tag works at the top of a page, inside a block, or in a layout: put
`{% members only %}` in `layouts/members.html` and every page that extends it is
members-only. To keep whole folders without touching each page, list them under
[`[access]` in friendo.toml](/docs/config#access).

**What a visitor sees.** The same URL, but your `pages/login.html` instead of the
page (status 401 if they're not signed in, 403 if they are but it's not enough).
Without a `login.html`, friendo shows a small built-in one: the site name, a line
about why, and `<friendo-signin>`. Once they sign in the page reloads and the real
content appears. A custom `login.html` gets a `gate` variable to explain itself:

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

`reload` on `<friendo-signin>` makes the page reload after signing in or out; use
it on any page that renders `{{ user }}` server-side. Members-only pages are left
out of a [static export](/docs/static-export), since a static host can't check
who's asking.

## Control flow

The usual tags are there, and they nest:

```html
{% for post in collections.blog %}
  <article>
    <h2>{{ post.title }}</h2>
    {% if post.fields.featured %}Featured{% elif post.status == "draft" %}Draft{% endif %}
  </article>
{% empty %}
  <p>No posts yet.</p>
{% endfor %}
```

- `{% for x in list %}…{% empty %}…{% endfor %}`; inside, `forloop.Counter`,
  `forloop.Counter0`, `forloop.First`, `forloop.Last`.
- `{% if %}…{% elif %}…{% else %}…{% endif %}` with `and`, `or`, `not`, comparisons and `in`.
- `{% set name = value %}`, `{% include "partial.html" %}`, `{# comments #}`,
  `{% raw %}…{% endraw %}`.

## Filters

Filters transform values with `|`. Friendo adds these to the standard Pongo2 set
(`truncatechars`, `truncatewords`, `upper`, `lower`, `title`, `length`, `first`,
`last`, `join`, `default`, `striptags`, `urlencode`, `safe`, …):

| Filter | Example | Result |
|---|---|---|
| `markdown` | `{{ post.body\|markdown }}` | markdown rendered to HTML (GitHub-flavored), not escaped |
| `date` | `{{ post.created\|date:"Jan 2, 2006" }}` | a formatted date |
| `asset_url` | `{{ "logo.png"\|asset_url }}` | `/assets/logo.png` |
| `resize` | `{{ img\|asset_url\|resize:"300x200" }}` | `/assets/img?w=300&h=200`, a hint an image CDN honours |
| `sort_by` | `collections.docs\|sort_by:"fields.weight"` | a list sorted by a (dotted) key, numerically when it can |
| `by_author` | `collections.blog\|by_author:"pat"` | the posts one [profile](/docs/signing-in#profiles) wrote |
| `by_following` | `collections.blog\|by_following:user` | the posts by people the viewer [follows](/docs/community#following) |
| `in_group` | `collections.blog\|in_group:"board"` | the posts filed under a [group](/docs/groups) |
| `upcoming`, `past`, `in_month`, `on_day` | `collections.events\|upcoming` | [events](/docs/calendar) by date, repeating ones expanded |
| `when` | `{{ event.when\|when:"2 January" }}` | an event's time with your own date layout (without a layout, `{{ event.when }}` already prints it) |
| `google_calendar_url` | `{{ event\|google_calendar_url }}` | Google Calendar's "add this event" link |

This site's sidebar is built with `sort_by`: each page is a post in a `docs`
collection, output through `{{ post.body|markdown }}` and ordered by `fields.weight`.
