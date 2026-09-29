---
title: Add a poll
slug: polls
section: Guides
topic: Content
weight: 40
description: Declare a poll in a post's front matter, drop <friendo-poll> on its page, and members vote once each.
---

To add a poll to a post, declare it in the post's front matter and put
`<friendo-poll>` on the post's page. There are no ids to copy: the poll is made
the first time the page is viewed.

## 1. Declare the poll

Give the poll a `slug`, a `question` and at least two `options`:

```yaml
---
title: Cats or dogs?
poll:
  slug: cats-or-dogs
  question: Cats or dogs?
  options: [Cats, Dogs]
  # closes_at: 2026-12-31T00:00:00Z   # optional
---
```

The slug names the poll across the whole site, so pick one no other post uses.
With `closes_at` set, votes stop at that time.

## 2. Show it on the page

In the template for the collection's posts:

```html
{% if post.fields.poll %}
  <friendo-poll poll-slug="{{ post.fields.poll.slug }}"></friendo-poll>
{% endif %}
```

A member clicks an option to vote. They vote **once**: after that the options
lock and they see the tally, with their own choice marked. A visitor who clicks
is asked to sign in.

## 3. Render the results without JavaScript

The tag loads in the browser. For results that readers and search engines see
without JavaScript, the same poll is on `post.poll`:

```html
{% if post.poll %}
  <h3>{{ post.poll.question }}</h3>
  <ul>{% for o in post.poll.options %}<li>{{ o.text }}: {{ o.votes }}</li>{% endfor %}</ul>
  <p>{{ post.poll.total_votes }} votes</p>
{% endif %}
```

Use both: the list for reading, the tag for voting. See
[Two spellings](/docs/two-spellings).

## Change or restart a poll

Change the question or option labels in the front matter and the running poll
updates in place; **existing votes are kept**. Votes are stored by option
position, so reorder options only before anyone has voted. To start over, give
the poll a new slug.

> **Common mistake:** a poll with an empty `question` or fewer than two
> `options` isn't made at all, and the tag has nothing to show.

Polls are a feature switch (`polls` in [`[settings]`](/docs/config#settings),
or admin **Settings → Features**). Switch it off and the tag renders nothing and
`post.poll` is empty. Poll votes are never [reviewed](/docs/review).

**Next:** [Style the tags](/docs/styling) to change how the poll looks, or the
[`<friendo-poll>` reference](/docs/components#friendo-poll).
