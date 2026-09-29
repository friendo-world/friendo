---
title: Tags and filters
slug: template-filters
section: Reference
weight: 50
description: The template tags, the members-only tags, and every filter friendo adds to Pongo2.
---

Templates are [Pongo2](https://github.com/flosch/pongo2), a Jinja2-style
language. `{% … %}` is a tag, `{{ … }}` prints a value, and `|` passes a value
through a filter. For the variables themselves, see
[Template variables](/docs/template-variables).

## Tags

| Tag | What |
|---|---|
| `{% extends "layouts/base.html" %}` | Build this page on a layout. Must come first |
| `{% block content %}…{% endblock %}` | A named slot a layout defines and a page fills |
| `{% include "layouts/nav.html" %}` | Insert another template here |
| `{% for x in list %}…{% empty %}…{% endfor %}` | Repeat for each item; `empty` runs when the list is empty |
| `{% if %}…{% elif %}…{% else %}…{% endif %}` | Choose. Conditions use `and`, `or`, `not`, `==`, `!=`, `<`, `>`, `<=`, `>=` and `in` |
| `{% set name = value %}` | Name a value for the rest of the template |
| `{% with name = value %}…{% endwith %}` | Name a value inside the block only |
| `{% macro name(arg) %}…{% endmacro %}` | A reusable snippet, called as `{{ name("x") }}` |
| `{# note #}` | A comment. It must fit on one line |
| `{% comment %}…{% endcomment %}` | A comment over several lines |
| `{% verbatim %}…{% endverbatim %}` | Print what's inside as written, `{{ }}` and all |
| `{% now "2006" %}` | The current time, in a Go date layout |

Inside a `for`: `forloop.Counter` (from 1), `forloop.Counter0` (from 0),
`forloop.First`, `forloop.Last`.

```html
{% for post in collections.blog %}
  <article>
    <h2>{{ post.title }}</h2>
    {% if post.fields.featured %}Featured{% endif %}
  </article>
{% empty %}
  <p>No posts yet.</p>
{% endfor %}
```

> **Common mistake:** `{# … #}` across two lines is an error ("Newline not
> permitted in a single-line comment"). Use `{% comment %}…{% endcomment %}`.

> **Common mistake:** `x not in y` is an error. Write `not (x in y)`, with the
> brackets: without them, `not x in y` reads as `(not x) in y` and is never true.

```html
{% if not ("board" in user.groups) %}<p>Ask to join the board.</p>{% endif %}
```

## Access tags

One tag keeps a page for the people it names. Put it at the top of a page, inside
a block, or in a layout (every page that extends the layout is then covered). See
[Members-only pages](/docs/members-only).

| Tag | Who gets in |
|---|---|
| `{% members only %}` | Anyone signed in |
| `{% contributors only %}` | Contributors and up |
| `{% moderators only %}` | Moderators and up |
| `{% editors only %}` | Editors and up |
| `{% admins only %}` | Admins and owners |
| `{% owners only %}` | Owners |
| `{% members only if user.email == "pat@example.com" %}` | Signed in **and** the condition holds. Any expression over `user`, `post`, … |
| `{% members only if "board" in user.groups %}` | Signed in **and** in the [group](/docs/groups) `board` |

Every spelling takes the `if` tail. A viewer who doesn't get in sees your
`pages/login.html` at the same URL (status 401 if not signed in, 403 if signed
in), with a [`gate`](/docs/template-variables#gate) variable; without one, a small
built-in sign-in page.

## Filters friendo adds

| Filter | Example | Result |
|---|---|---|
| `markdown` | `{{ post.body\|markdown }}` | Markdown rendered to HTML (GitHub-flavored, headings get ids), not escaped |
| `asset_url` | `{{ "logo.png"\|asset_url }}` | `/assets/logo.png` |
| `resize` | `{{ "photo.jpg"\|asset_url\|resize:"300x200" }}` | `/assets/photo.jpg?w=300&h=200`. Also `"300"` or `"x200"`. A hint an image CDN honours; the built-in server sends the original |
| `sort_by` | `collections.docs\|sort_by:"fields.weight"` | The list sorted by a (dotted) key, numerically when both values are numbers |
| `by_author` | `collections.blog\|by_author:"pat"` | The posts one [profile](/docs/profiles) wrote, by its slug or id |
| `by_following` | `collections.blog\|by_following:user` | The posts by people the viewer follows; empty for a visitor |
| `in_group` | `collections.blog\|in_group:"board"` | The posts filed under a [group](/docs/groups) |
| `upcoming` | `collections.events\|upcoming` or `upcoming:5` | Dates from now through the next twelve months, soonest first, at most 5 |
| `past` | `collections.events\|past` or `past:5` | Dates already started, within the last twelve months, most recent first |
| `in_month` | `collections.events\|in_month:"2026-10"` | Dates in a month (this month if blank) |
| `on_day` | `collections.events\|on_day:"2026-10-04"` | Dates on a day (today if blank) |
| `when` | `{{ event.when\|when:"2 January" }}` | An event's time with your own Go date layout. `{{ event.when }}` alone reads `Sat Oct 4, 10 am – 4 pm` |
| `google_calendar_url` | `{{ event\|google_calendar_url }}` | Google Calendar's pre-filled "add this event" link; empty for a post with no time |

`upcoming`, `past`, `in_month` and `on_day` **expand** a repeating event into one
entry per date. Each entry is the post with `when` set to that date, so
`{{ e.slug }}` still links to the one post. To order the unexpanded list by time,
use `sort_by:"when.start"`. See [Calendar](/docs/calendar).

Filters chain left to right:

```html
{% for e in collections.events|in_group:"board"|upcoming:3 %}
  <li><a href="/events/{{ e.slug }}">{{ e.title }}</a> · {{ e.when }}</li>
{% endfor %}
```

## Pongo2's standard filters

These come with Pongo2 and work as its
[documentation](https://github.com/flosch/pongo2) describes:

`add`, `addslashes`, `capfirst`, `center`, `cut`, `default`, `default_if_none`,
`divisibleby`, `escape`, `escapejs`, `first`, `float`, `floatformat`,
`get_digit`, `integer`, `iriencode`, `join`, `last`, `length`, `length_is`,
`linebreaks`, `linebreaksbr`, `linenumbers`, `ljust`, `lower`, `make_list`,
`phone2numeric`, `pluralize`, `random`, `removetags`, `rjust`, `safe`, `slice`,
`split`, `stringformat`, `striptags`, `time`, `title`, `truncatechars`,
`truncatechars_html`, `truncatewords`, `truncatewords_html`, `upper`,
`urlencode`, `urlize`, `urlizetrunc`, `wordcount`, `wordwrap`, `yesno`.
