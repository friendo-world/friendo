---
title: Use the tags from your own JavaScript
slug: sdk-events
section: Guides
topic: Community
weight: 80
description: Listen for the friendo:* events the tags fire, to react when someone signs in, posts, answers or follows.
---

To run your own code when something happens in a `<friendo-*>` tag, listen for
the `friendo:*` DOM events it fires. Each carries what happened in
`event.detail`.

## 1. Listen for an event

Put a script after `/friendo.js`. Events that bubble can be caught on
`document`, so one listener covers every tag on the page:

```html
<script src="/friendo.js" defer></script>
<script>
  document.addEventListener("friendo:submitted", (e) => {
    console.log("New post:", e.detail.post.title);
  });
</script>
```

## 2. Send visitors to sign in

When a visitor clicks something only members can do, the tag fires
`friendo:needs-auth`. Catch it and scroll to your sign-in:

```html
<script>
  document.addEventListener("friendo:needs-auth", () => {
    document.querySelector("friendo-signin").scrollIntoView({ behavior: "smooth" });
  });
</script>
```

## 3. Follow who's signed in

`friendo:signin` fires on `document` when a member signs in or out.
`detail.user` is the member, or `null` after signing out. Every tag on the page
refreshes by itself, so listen only for your own parts of the page.

If your page renders `{{ user }}` on the server, add `reload` to
`<friendo-signin>` instead, and the page reloads after signing in or out.

## Every event

| Event | Fired by | On | `detail` |
|---|---|---|---|
| `friendo:signin` | `<friendo-signin>`, after a code checks out or on *Sign out* | `document` | `user`: the member, or `null` |
| `friendo:needs-auth` | `<friendo-reactions>`, `<friendo-poll>`, `<friendo-rsvp>`, `<friendo-follow>`, when a visitor clicks | the tag, bubbles | none |
| `friendo:submitted` | `<friendo-form>`, after it makes a post | the tag, bubbles | `post`: the new post |
| `friendo:rsvp` | `<friendo-rsvp>`, after an answer | the tag, bubbles | `postId`, `date`, `answer` (`going`, `maybe` or `not_going`) |
| `friendo:follow` | `<friendo-follow>`, after a click | the tag, bubbles | `profileId`, `following` (`true` or `false`) |
| `friendo:profile` | `<friendo-signin>` or `<friendo-profile>`, after a profile is saved | `document` | `profile`: the saved profile |
| `friendo:notifications` | `<friendo-inbox>`, after *mark read* or *Mark all read* | `document` | `unread`: how many are left |
| `friendo:group` | `<friendo-group>` after a change, `<friendo-groups>` after making a group | the tag, bubbles | `groupId`; also `created: true` for a new group |
| `friendo:invite` | `<friendo-invite>`, after inviting | the tag, bubbles | `postId`, `invited`: how many were invited |

A network's own tags fire two more: `friendo:account` on `document` when
someone signs in or out of a network account (`detail.account`, or `null`), and
`friendo:site-created` from `<friendo-account>` after making a site
(`detail.subdomain`). See [Run a network](/docs/run-a-network).

## Tags that listen

Some tags already listen for you:

- Every tag re-renders on `friendo:signin`.
- `<friendo-calendar>` refetches on `friendo:submitted`, and opens the month of a
  newly published event.
- `<friendo-signin>` updates its unread badge on `friendo:notifications`.

> **Common mistake:** `friendo:signin`, `friendo:profile` and
> `friendo:notifications` fire on `document` and don't bubble. A listener on a
> tag or a wrapper element never hears them; put it on `document`.

**Next:** [Style the tags](/docs/styling), or the
[Components](/docs/components) reference for every tag's attributes.
