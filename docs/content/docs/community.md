---
title: Community features
slug: community
section: Concepts
weight: 8
---

Friendo sites can host **comments, reactions, polls, chats and more**, written by
your members, with no custom JavaScript. You drop in a tag, a visitor signs in
with their email, and they can join in. This page shows how; the words behind it
(member, role, profile) are in [Signing in & roles](/docs/signing-in).

## One script, then tags

Every site serves the tags at **`/friendo.js`**. Load it once, in your layout, and
use them anywhere:

```html
<script src="/friendo.js" defer></script>

<friendo-signin></friendo-signin>
<friendo-comments post-id="{{ post.id }}"></friendo-comments>
<friendo-reactions post-id="{{ post.id }}"></friendo-reactions>
<friendo-map post-id="{{ post.id }}"></friendo-map>
```

Inside a post's page, `{{ post.id }}` is the post's stable id, and every tag that
works on a post takes it as `post-id`. The tags talk to the site's own API
themselves; nothing else is needed.

| Tag | What it does |
|---|---|
| `<friendo-signin>` | Sign in with an email code; shows who's signed in, their profiles, and a sign-out button. Add `reload` on pages that render `{{ user }}`. |
| `<friendo-comments post-id>` | The approved comments and, for members, a box to write one. |
| `<friendo-reactions post-id>` | Emoji reactions with live counts; a click toggles yours. |
| `<friendo-poll poll-slug>` | A poll; members vote once and see the tally. |
| `<friendo-chat chat-id>` | A realtime message feed. See [Chats](/docs/chats). |
| `<friendo-map post-id>` | A map of the post's [location](/docs/locations); with no `post-id`, every post. |
| `<friendo-form collection>` | A form that makes a new post. See [below](#posting-from-a-page). |
| `<friendo-calendar>` | A month grid or list of the site's [events](/docs/calendar). |
| `<friendo-rsvp post-id>` | Going / Maybe / Can't go on an [event](/docs/calendar#rsvp). |
| `<friendo-add-to-calendar>` | A menu of calendar apps for one event or the whole feed. |
| `<friendo-invite post-id>` | The organizer's side of an RSVP: invite people. |
| `<friendo-profile slug>` | A member's [profile](/docs/signing-in#profiles), with an *Edit profile* button on your own. |
| `<friendo-follow profile-id>` | A [follow](#following) button with the follower count. |
| `<friendo-inbox>` | The member's [notifications](#notifications). |
| `<friendo-group post-id>` | One [group](/docs/groups): join, the members, the admins' levers. |
| `<friendo-groups>` | The groups the viewer may see, and a *start a group* form. |

Every attribute, part and event is listed in the [Components](/docs/components) reference.

## Two spellings of the same thing

The tags are interactive and load client-side. For content that should be readable
**without JavaScript** (approved comments, reaction tallies, a poll's results) the
runtime also attaches the same data to `post`, so a template can render it inline:

```html
<h2>Reactions</h2>
<ul>{% for r in post.reactions %}<li>{{ r.emoji }} {{ r.count }}</li>{% endfor %}</ul>

{% if post.poll %}
  <h3>{{ post.poll.question }}</h3>
  <ul>{% for o in post.poll.options %}<li>{{ o.text }} — {{ o.votes }}</li>{% endfor %}</ul>
{% endif %}

<h2>Comments</h2>
<ol>{% for c in post.comments %}<li><b>{{ c.author_name }}</b>: {{ c.body }}</li>{% endfor %}</ol>
```

Use whichever a page needs, often both: the server-rendered list for readers and
search engines, and the tag for signing in and joining in. Chats and maps are live
by nature, so they're tags only. See [What a post carries](/docs/templates#what-a-post-carries).

## Members

A visitor becomes a **member** by entering their email in `<friendo-signin>` and
typing the code it sends; no password. In local development (no email provider)
the code is shown right in the form. See [Sending email](#sending-email) to wire
up a real provider.

Members can comment, react, vote, RSVP, follow and join groups. Whether they can
post, and whether their posts wait for review, is up to your
[settings](/docs/signing-in#presets).

## Review

New comments wait for **review** until a moderator approves them (turn off
**Comments wait for review** in admin **Settings → Comments** and they show at
once). The admin's **Review** page lists comments and posts that are waiting;
each can be approved, rejected or deleted.

You can review right on the page, too: when the signed-in viewer is the post's
author or a site moderator, `<friendo-comments>` shows waiting comments with
inline **Approve / Reject / Delete** buttons. A member sees their own waiting
comment marked as such, and can delete anything they wrote.

Reactions and poll votes are never reviewed: they're one per member and simply toggle.

## Adding a poll

Declare a poll in a post's front matter and give it a `slug`. The runtime makes
the poll the first time it's viewed; no ids to copy:

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

```html
{% if post.fields.poll %}
  <friendo-poll poll-slug="{{ post.fields.poll.slug }}"></friendo-poll>
{% endif %}
```

Change the question or option labels in the front matter and the running poll
updates in place; **existing votes are kept**. To start over, give the poll a new
slug.

## Posting from a page

Comments and reactions let people add *to* a post. `<friendo-form>` lets them make
*a whole post*: a title, a body and any fields, straight from a page, with no
admin and no schema. You write an ordinary form; the tag turns it into a post.

```html
<friendo-form collection="blog" redirect="/blog/{slug}">
  <input name="title" placeholder="Title" required />
  <friendo-input name="body" type="richtext" placeholder="Write your story…"></friendo-input>
  <friendo-input name="where" type="location"></friendo-input>
  <friendo-input name="cover" type="media" accept="image/*"></friendo-input>
  <friendo-input name="tags" type="tags"></friendo-input>
  <select name="mood"><option>calm</option><option>hyped</option></select>
  <button type="submit">Publish</button>
</friendo-form>
```

**Field names decide where values land.** `title`, `body`, `slug` and `status` are
the post's own parts; `when` makes it an [event](/docs/calendar#let-people-submit-events);
every other name becomes a field. The form above makes a post whose fields are
`{ where: {lat, lng}, cover: "<url>", tags: [...], mood: "calm" }`, and those read
straight back in a template as `{{ post.fields.mood }}`.

Native `<input>`, `<textarea>` and `<select>` work as-is. For richer values, a
`<friendo-input>` with a `type`:

| `type` | Gives you | Value |
|---|---|---|
| `richtext` | a WYSIWYG editor (bold, headings, lists, links) | markdown, so it fits `body` |
| `location` | a click-to-pick map | `{ lat, lng }`; the post gets a [location](/docs/locations) too |
| `media` | a file picker with preview | the uploaded image's URL (contributors and up) |
| `tags` | a chip input | a list of strings |
| `when` | start, end, all-day and repeats in one control | the post's [`when`](/docs/calendar) |

The rich inputs load their libraries from a CDN the first time they appear; if
`richtext` can't reach it, it falls back to a plain textarea, so the form always works.

**After submit.** Give the form a `redirect` and it goes there, with `{slug}` and
`{id}` filled in from the new post. Without one it resets and shows a message.
Either way it fires a `friendo:submitted` event whose `detail.post` is the new post.

**Who can post.** Contributors and up, by default; their post publishes or waits
according to **Contributors' posts wait for review**. Plain members can post once
you turn on **Members can post** in admin **Settings → Members & roles**; their
posts always wait for review, and are rate-limited.

## Following

Members follow each other, one way, like a subscription. A **friend** is two
people who follow each other; friendo works that out, there's nothing to accept.
Drop the button on a [profile page](/docs/signing-in#profiles):

```html
<friendo-follow profile-id="{{ profile.id }}"></friendo-follow>
```

It shows *Follow* (or *Following*, pressed) and the follower count; on your own
profile, just the count. `slug="pat"` works in place of `profile-id`.

Templates see the graph without JavaScript. On a profile page `profile.followers`
and `profile.following` are lists of profiles, with `profile.follower_count` and
`profile.following_count`. The viewer carries `user.following`, `user.followers`
and `user.friends`, and the `by_following` filter turns that into a feed:

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

Follows travel with `friendo push --users`. The whole thing is the **Follows**
switch in **Settings → Features**.

## Notifications

Everything social needs a way to tell a member something happened. In friendo
that's an **inbox they see when they visit**; no email beyond the sign-in code.
A note is written when someone follows you, comments on your post (once the
comment is approved), asks to join a group you moderate, adds you to a group, or
invites you to an event.

```html
{# pages/inbox.html #}
{% members only %}
<friendo-inbox></friendo-inbox>
```

`<friendo-inbox>` lists them newest first, unread in bold, with *mark read* and
*Mark all read*. `<friendo-signin>` shows an unread badge, and templates get
`{{ user.unread }}` for a badge of your own. `friendo init` scaffolds the inbox
page and a nav link. The inbox is on while **Follows** or **Groups** is.

## Uploading images

Attach an image to any post through the admin, a `<friendo-input type="media">`,
or the [files API](/docs/api#files). Files land in your site's `assets/uploads/`
folder and are served from `/assets/`, the same locally and deployed; `friendo
push --posts` carries them along. Only images are accepted, up to 10 MB.

## Styling

Each tag renders into a shadow root and ships almost no styling of its own. Style
it from your stylesheet through the parts it exposes:

```css
friendo-comments::part(submit) { background: rebeccapurple; color: #fff; }
friendo-reactions::part(button)[aria-pressed="true"] { background: #eef; }
friendo-poll::part(bar) { background: #eef; }
```

Reaction, poll and RSVP buttons carry `aria-pressed="true"` for the member's own
choice. `<friendo-form>` is the one tag that stays in your page's DOM (so your
inputs stay yours); style its `status` and `error` parts by attribute:
`friendo-form [part="error"] { … }`. Every part is listed in
[Components](/docs/components).

## From your own JavaScript

The tags fire DOM events you can listen for:

- **`friendo:signin`** on `document` when a member signs in or out; `detail.user`
  is the member or `null`. Every tag on the page refreshes by itself.
- **`friendo:needs-auth`** bubbles from a tag when a visitor tries to act. Catch it
  to scroll to your `<friendo-signin>`.
- **`friendo:submitted`** bubbles from `<friendo-form>` after it makes a post;
  `detail.post` is the post.
- **`friendo:rsvp`** bubbles from `<friendo-rsvp>` after an answer; `detail` has
  `postId`, `date` and `answer`.

## Sending email

Until an email provider is configured, `<friendo-signin>` shows the sign-in code
in the form (handy locally). To send real email, set two environment variables on
the runtime:

```
RESEND_API_KEY=…            # a Resend API key
FRIENDO_EMAIL_FROM=Your Site <hello@yoursite.example>
```

With both set, codes are emailed and never shown. One live code per address at a time.
