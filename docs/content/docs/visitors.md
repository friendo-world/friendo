---
title: Let visitors take part
slug: visitors
section: Guides
topic: Community
weight: 15
description: Let people react, vote, RSVP, comment and post without signing in first, and keep what they did when they do sign in.
---

By default, only members can react, comment, vote in a poll, RSVP or post from a
form. A visitor who tries gets asked to sign in. You can let visitors do any of
these straight away instead, with one switch each.

## Turn it on

In admin **Settings**, turn on the ones you want: **Visitors can react**,
**Visitors can vote**, **Visitors can RSVP**, **Visitors can comment** and
**Visitors can post**. Or set them in `friendo.toml`:

```toml
[settings]
visitors_can_react = true
visitors_can_vote = true
visitors_can_rsvp = true
visitors_can_comment = true
visitors_can_post = true
visitors_can_upload = true
```

Your pages don't change. `<friendo-reactions>`, `<friendo-poll>`,
`<friendo-rsvp>`, `<friendo-comments>` and `<friendo-form>` notice the switch and
let visitors in.

## What a visitor sees

- **Reactions and votes** count straight away.
- **RSVPs** ask for a name the first time, so whoever organizes the event knows
  who's coming.
- **Comments** have a box for an optional name. A visitor's comment always
  [waits for review](/docs/review), even when members' comments don't. Until
  it's approved, only the visitor sees it, and they can delete it.
- **Posts** from a [`<friendo-form>`](/docs/forms) always wait for review. Add an
  input named `author_name` to ask for a name. While the post waits, the form
  offers **Take it back**.
- **Images** in a post need one more switch, **Visitors can add images**
  (`visitors_can_upload`). They go to review with the post, and they're
  removed if the visitor takes the post back.
- Wherever a visitor's name shows, it's marked: "Robin (visitor)", or just
  "Visitor" if they gave none. A visitor can't use a name a member already has.
- After they act, the tag says **Sign in to keep this**. Style or hide that line
  with `::part(visitor)`.

In a template, [`visitor`](/docs/template-variables#visitor) says what visitors
may do, so a page can explain itself:

```html
{% if visitor.can.comment %}<p>No account needed to comment.</p>{% endif %}
```

## Signing in keeps it

The visitor's browser remembers what they did for a year. When they sign in:

- **With a new email,** they become a member and everything they did is theirs.
- **With an email that already has an account,** what they did moves onto that
  account. Where both did the same thing, like voting in the same poll, the
  account's own answer is kept.

If they clear their browser's cookies first, what they did stays but can't be
carried over.

## Drop boxes

Sometimes nobody should know who sent something: a tip line, anonymous
feedback, a suggestion box. Add `drop-box` to the form:

```html
<friendo-form collection="tips" drop-box>
  <textarea name="body" required></textarea>
  <friendo-input name="photo" type="media"></friendo-input>
  <button type="submit">Send</button>
</friendo-form>
```

That makes `tips` a **drop box**: it takes posts from anyone, signed in or not,
whatever the switches above say.

- **It's the whole collection, not just this form.** Every post to `tips` keeps
  no name, from any form and from the admin. A plain
  `<friendo-form collection="tips">` elsewhere is a drop box too; the browser's
  console says so.
- **Write the collection out plainly.** friendo reads `drop-box` from your
  template files when the site loads, before any page renders, so
  `collection="{{ … }}"` can't count. Take the attribute out and the collection
  stops being a drop box.
- **Removing it is loud.** If the last `drop-box` form for a collection goes
  away, friendo logs a warning and the admin's page for that collection says new
  posts there keep their sender's name, until an admin dismisses it.

The difference from a visitor's post:

- **No name is kept, for anyone.** Not a visitor's, not a signed-in member's,
  not even an admin's. A moderator sees *no name kept*.
- **Nothing ties the post to the person.** Sending one doesn't start a visitor,
  so there's nothing to carry over when they sign in.
- **It always waits for review.**
- **Nobody can take it back,** since nobody owns it.
- **Images can come along.** A `media` input in the form works for anyone, no
  switch needed. The images wait for review with the post.

friendo itself keeps no log of who sent what. If your site sits behind another
service (Cloudflare, a hosting proxy), that service may keep its own request logs.

## Keeping it fair

- Following and groups still need a member. A chat does too, unless its tag
  says `visitors-can-chat`; see [Let visitors chat](/docs/chats#let-visitors-chat).
- Everything a visitor writes waits for a moderator. In admin **Review**, tick
  **Only from visitors** to look at just those.
- A visitor gets one vote per browser, so someone determined can vote twice with
  a second browser. Keep polls that matter for members.
- friendo limits how many new visitors, and how many actions, come from one
  network address in a short time, and quietly drops comments and posts from
  bots that fill in a hidden field people never see.
- An image on a post that isn't published yet is private: only moderators,
  editors and whoever sent it can open it. Anyone else gets *not found*, so a
  link to an unreviewed image can't be passed around.
- Visitors don't appear in admin **Members** and have no profile page. A visitor
  never moderates, even comments on a post of theirs that was approved.
