---
title: Let members post from a page
slug: forms
section: Guides
topic: Content
weight: 30
description: Put a <friendo-form> on any page and the people signed in to your site can make a whole post from it, with no admin and no schema.
---

To let people make a post from one of your pages, wrap an ordinary form in
`<friendo-form>`. Comments and reactions add *to* a post; `<friendo-form>` makes
*a whole post*: a title, a body and any fields you like.

## 1. Put the form on a page

A site made with `friendo init` already loads `/friendo.js` and shows
`<friendo-signin>` in its layout. Add the form to any page:

```html
<friendo-form collection="blog" redirect="/blog/{slug}">
  <input name="title" placeholder="Title" required />
  <friendo-input name="body" type="richtext" placeholder="Write your story…"></friendo-input>
  <friendo-input name="place" type="location"></friendo-input>
  <friendo-input name="cover" type="media" accept="image/*"></friendo-input>
  <friendo-input name="tags" type="tags"></friendo-input>
  <select name="mood"><option>calm</option><option>hyped</option></select>
  <button type="submit">Publish</button>
</friendo-form>
```

`collection` is where the post goes. Native `<input>`, `<textarea>` and
`<select>` work as they are. A `<button type="submit">` sends the form, and so
does Enter in a one-line input.

## 2. Name the fields

**Field names decide where values land.** `title`, `body`, `slug` and `status`
are the post's own parts; `when` makes it an [event](#events-from-a-form); every
other name becomes a field. The form above makes a post whose fields are
`{ place: {lat, lng}, cover: "<url>", tags: [...], mood: "calm" }`, and they read
straight back in a template as `{{ post.fields.mood }}`.

Leave out `slug` and one is made from the title.

## 3. Add richer inputs

For values a native input can't give you, use a `<friendo-input>` with a `type`:

| `type` | Gives you | Value |
|---|---|---|
| `richtext` | a WYSIWYG editor (bold, headings, lists, links) | markdown, so it fits `body` |
| `location` | a click-to-pick map | `{ lat, lng }`; the post gets a [location](/docs/locations) too |
| `media` | a file picker with preview | the uploaded image's URL (contributors and up; members and visitors when allowed, see below) |
| `tags` | a chip input | a list of strings |
| `when` | start, end, all-day and repeats in one control | the post's [`when`](/docs/calendar) |

The rich inputs load their libraries from a CDN the first time they appear. If
`richtext` can't reach it, it falls back to a plain textarea for markdown, so the
form always works. Images from `media` are covered in
[Upload images](/docs/images).

## 4. Decide what happens after submit

Give the form a `redirect` and it goes there, with `{slug}` and `{id}` filled in
from the new post. Without one it clears itself and shows *Submitted.*

Either way it fires a `friendo:submitted` event whose `detail.post` is the new
post; see [Use the tags from your own JavaScript](/docs/sdk-events). A
`<friendo-calendar>` on the same page refreshes by itself when it hears one.

The form asks for the status `published` unless you set `status="draft"` on the
tag (or put a field named `status` in the form).

## 5. Decide who can post

- **Contributors and up** can post by default. Their post publishes, or waits
  for [review](/docs/review) when **Contributors' posts wait for review** is on.
- **Plain members** can post once you turn on **Members can post** in admin
  **Settings → Members & roles** (`members_can_post` in
  [`friendo.toml`](/docs/config#settings)). Their posts always wait for review,
  and they're rate-limited.
- **Visitors** (not signed in) can post once you turn on **Visitors can post**
  (`visitors_can_post`). Their posts always wait for review. Add an input named
  `author_name` if you'd like them to give a name. With it off, the form tells
  them *Please sign in before posting.* See [Let visitors take part](/docs/visitors).

While a post waits for review, the form shows **Take it back**, so whoever sent
it, member or visitor, can withdraw it, along with any images sent with it.

Images from a `media` input are for contributors and up. To let others attach
them, turn on **Members can add images** (`members_can_upload`) or **Visitors can
add images** (`visitors_can_upload`). Their images wait for review along with the
post, and each post can carry up to 10. Without the switch, the picker shows a
note instead.

> **Common mistake:** a signed-in member sees *Your account can't post here.*
> That's a member without a role on a site where **Members can post** is off.
> Turn it on, or make them a contributor in the admin's **Members** page.

## Events from a form

A `<friendo-form>` with a `when` field makes an event. An ordinary date input is
all it takes; dotted names fill in the rest of the `when`:

```html
<friendo-form collection="events">
  <input name="title" placeholder="What's happening?" required>
  <input name="when" type="datetime-local" required>
  <input name="when.end" type="datetime-local">
  <select name="when.repeats">
    <option value="">Once</option><option>weekly</option><option>monthly</option>
  </select>
  <friendo-input name="body" type="richtext"></friendo-input>
  <friendo-input name="place" type="location"></friendo-input>
  <button type="submit">Submit event</button>
</friendo-form>
```

The input named `when` is the start; `when.end`, `when.repeats` and any other
`when.<key>` join it. For one control with start, end, all-day and repeats
together, use `<friendo-input name="when" type="when">` in place of the native
fields.

Contributors publish directly. With **Members can post** on, a member's event
waits for review like any post, with its date shown, until a moderator approves
it.

The [API](/docs/api#posts) works the same way: send `"when"` inside `fields`, as
a string or a map, and it comes back on the post as `when`. An update that says
nothing about `when` leaves the event alone; send `"when": null` to remove it.

**Next:** [Review comments and posts](/docs/review) to approve what members send,
or the [`<friendo-form>` reference](/docs/components#friendo-form) for every
attribute and part.
