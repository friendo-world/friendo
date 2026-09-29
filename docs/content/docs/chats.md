---
title: Add a chat
slug: chats
section: Guides
topic: Community
weight: 40
description: A realtime message feed on any page, post or group.
---

A **chat** is a realtime message feed: a chat room, a live thread under an event.
Members post; new messages stream in live, no polling.

## Put one on a page

```html
<friendo-signin></friendo-signin>
<friendo-chat chat-id="general"></friendo-chat>
<script src="/friendo.js" defer></script>
```

That's the whole feature. Naming a chat is making it: the first time a page with
`chat-id="general"` is served, the site makes a chat called `general` if there
isn't one yet. There's nothing to set up first.

Visitors see the messages and a "Sign in to join" line; members get a composer and
can delete their own messages. It looks like a phone's messages out of the box:
your bubbles on the right, everyone else's on the left, a time stamp above each
batch of messages, the composer along the bottom. Enter sends; Shift+Enter makes a
new line. Tap a message to see exactly when it was sent and, on your own (or any,
for a moderator), a delete link.

Tune it from your stylesheet with custom properties on the tag:

```css
friendo-chat {
  --chat-mine: #6d5dfc;      /* your bubbles and the Send button */
  --chat-theirs: #e9e9eb;    /* everyone else's bubbles */
  --chat-border: #d9d9de;
  --chat-background: #fff;
  --chat-height: 60vh;       /* the list, before it scrolls */
}
```

For anything else, style the parts with `::part()`; they're listed in
[Components](/docs/components#friendo-chat).

## One chat per post

Because the chat is made from whatever the rendered page names, a template can
name one per post, a live thread under every event, say:

```html
<friendo-chat chat-id="event-{{ event.slug }}"></friendo-chat>
```

Each event's page makes its own chat the first time someone opens it. A chat id is
a slug: letters, digits, dots, dashes and underscores, up to 64 characters.

Only what your own templates render counts, so a visitor can't make chats by
posting a comment (unless a template renders their text unescaped with `|safe`,
which it shouldn't).

## Group chats

A [group](/docs/groups) can have chats of its own, and they're **for its members
only**: reading, the live stream and posting all need membership. Name the group
on the tag:

```html
<friendo-chat chat-id="general" group="{{ group.slug }}"></friendo-chat>
```

Chat ids are unique within a group, so every group can have a `general`. A member
who isn't in the group sees *Join the group to chat* in place of the messages.
The group's admins add and remove its chats from `<friendo-group>`; its moderators
(and the site's) can delete any message. On the group's page, `group.chats` lists
them:

```html
{% for c in group.chats %}
  <h3>{{ c.name }}</h3>
  <friendo-chat chat-id="{{ c.id }}" group="{{ group.slug }}"></friendo-chat>
{% endfor %}
```

Both the **Chats** and **Groups** switches must be on. A group's chats go with the
group when it's deleted.

## Limits and who can delete

Posting is rate-limited per member (the same limit as comments). A site moderator
can delete any message; members delete only their own. A deleted message
disappears from every open page, the same way a new one arrives. A chat is
inherently live, so it's the one community feature with no server-rendered form.

Admins can also make a chat ahead of time or give it a display name through the
[API](/docs/api#chats).
