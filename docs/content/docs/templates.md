---
title: How templates work
slug: templates
section: Concepts
weight: 30
description: Pages are plain template files that render posts. What's in the editor is what's in the file.
---

A friendo page is a plain `.html` file with a few tags in it. Friendo templates
are [Pongo2](https://github.com/flosch/pongo2), a Jinja2-compatible language, so if
you've seen Django, Jinja, Twig or Nunjucks you already know the shape. One
runtime renders them everywhere, so a template looks the same on your laptop, on
a server you run, and on friendo.world.

## Layouts and blocks

Put shared chrome in `layouts/` and extend it from pages. A layout is a page
with holes in it; a page fills the holes.

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
`pages/blog/index.html` is `/blog`. There's no routing table to keep in step with
the files; the folder is the site map.

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
the same thing. One folder is special: `pages/profiles/[slug].html` renders a
member's [profile](/docs/profiles), not a post, and there the profile is `profile`.

## What a page sees

Every page gets the site (`site.name`), the request, every published post by
collection (`collections.blog`), the signed-in member as `user` (empty for a
visitor), and on a post's page the post with everything the community has added
to it: comments, reactions, its poll, its RSVPs, its gallery. All of it is
attached before the template runs, so a page renders complete with no JavaScript.
The full list is [Template variables](/docs/template-variables); the tags and
filters you can use on them are in [Tags and filters](/docs/template-filters).

## Honest templates

What's in the editor is what's in the file. There's no build step, no compiled
output and no hidden state: a template is plain text you can check into git,
diff, and open in any editor. That's a deliberate trade. Friendo won't hide the
markup from you, and in return it never surprises you with markup you didn't
write.

The same honesty runs through the [members-only](/docs/members-only) tag: one
line at the top of a page says who it's for, and a visitor gets your `login.html`
instead. Nothing is enforced somewhere you can't see.

**See also:** [Posts and collections](/docs/posts) for what a template renders,
and [Server-rendered and live](/docs/two-spellings) for when to use a `<friendo-*>`
tag instead of a template variable.
