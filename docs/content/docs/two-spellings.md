---
title: Server-rendered and live
slug: two-spellings
section: Concepts
weight: 40
description: What members add to a post reaches your page two ways, as template data the server renders and as a <friendo-*> tag people use, and why you often want both.
---

Comments, reactions, polls and the rest reach a page in two spellings. The
**server-rendered** spelling is data on `post`, printed by your template before the
page leaves the server. The **live** spelling is a `<friendo-*>` tag that loads in
the browser and lets people join in. They show the same things; they're for
different readers.

## Content is rendered by the server

Anything a reader or a search engine should see is attached to the post before your
template runs, so it's in the HTML with no JavaScript at all:

```html
<ul>{% for r in post.reactions %}<li>{{ r.emoji }} {{ r.count }}</li>{% endfor %}</ul>
<ol>{% for c in post.comments %}<li><b>{{ c.author_name }}</b>: {{ c.body }}</li>{% endfor %}</ol>
{% for img in post.gallery %}<img src="{{ img.url }}" alt="">{% endfor %}
```

`post.comments` is the approved comments, oldest first. `post.reactions` is the
tallies. `post.poll` is the question and each option's votes, when the front matter
declares a poll. `post.rsvps` is an event's count of going, maybe, can't go and
invited for its next date. `post.gallery` is the images in the post's folder. On a
profile page, `profile.followers` and `profile.following` are the people, with
their counts. `post.author` and, on a group's page, `group.members` are there too.

These are plain lists. You decide the markup, they're fast, they work with
JavaScript off, and a search engine reads them like any other text. What they
can't do is change: they show the page as it was when it was served, and they
show it to nobody in particular. A reaction's "did I react?" or a poll's "my vote"
isn't there, because the server-rendered spelling is the same for everyone.

## Interaction is a tag

Doing something needs the live spelling. `<friendo-comments>` signs a member in and
lets them write, `<friendo-reactions>` toggles their emoji, `<friendo-poll>` takes
their vote, `<friendo-rsvp>` their answer, `<friendo-follow>` their follow. Each tag
talks to the site's API itself, knows who's looking, and redraws when they act.

So a post's page often uses both: the server-rendered list for reading, and the tag
beside it for joining in.

```html
<h2>Comments</h2>
<ol>{% for c in post.comments %}<li><b>{{ c.author_name }}</b>: {{ c.body }}</li>{% endfor %}</ol>
<friendo-comments post-id="{{ post.id }}"></friendo-comments>
```

Friendo doesn't try to make the two identical. The goal is one obvious tag for each
interactive thing, and plain template data wherever the content earns a place in
the HTML. Every tag is listed in [Components](/docs/components); every variable in
[Template variables](/docs/template-variables).

## Chats and maps are tags only

Some things are live by nature. A [chat](/docs/chats) is a stream of messages
arriving as they're written, and a [map](/docs/locations) is something you drag
and zoom. Neither has a server-rendered list, so they're `<friendo-chat>` and
`<friendo-map>` and nothing else. A post's place is still data, though:
`post.location` carries its `lat`, `lng` and `label` for a template to print.

## The same switch covers both

A feature turned off in **Settings → Features** goes quiet in both spellings at
once: its tag renders nothing and its list on `post` is empty, so
`{% if post.comments %}` stays false. What people already wrote is kept for when it
comes back.

## In a static export

A [static export](/docs/static-export) has no server behind it, so no tag that
signs people in or saves anything works there. What the export keeps is what it
rendered: each post's `author`, `when`, `location` and group, a group's
`members`, and on profile pages (when profiles are public) the posts, followers and
counts.

Comments, reactions, polls, RSVP counts and gallery images are attached only by the
running server, when it renders a post's page. In an export those lists are empty,
so a page that must carry them as text should be served by friendo, not exported.

**Next:** [Components](/docs/components) for every tag, or
[The social graph](/docs/social-graph) for where follows and groups come from.
