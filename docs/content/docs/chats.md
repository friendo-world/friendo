---
title: Chats (realtime)
slug: chats
group: Concepts
weight: 9
---

A **chat** is a realtime message feed — a chat room, a comments-style stream,
a live thread under an event. Signed-in members post; new messages stream in
live over a single server-sent-events connection, no polling.

## Put one on a page

```html
<friendo-auth></friendo-auth>
<friendo-chat chat-id="general"></friendo-chat>
<script src="/friendo.js" defer></script>
```

That's the whole feature. Naming a chat is making it: the first time a page with
`chat-id="general"` is served, the site registers a chat called `general` if
there isn't one yet. There's nothing to set up first.

Signed-out visitors see the messages and a "Sign in to join" line; signed-in
members get a composer, and can delete their own messages. It looks like a
phone's messages out of the box: a bordered box, your bubbles on the right in
the accent colour, everyone else's on the left in grey, the composer along the
bottom. Enter sends; Shift+Enter makes a new line. The list scrolls like a chat
— newest at the bottom, opening there and following new messages unless you've
scrolled up to read. Times aren't on every bubble: a stamp sits above each batch
of messages ("just now", "5 min ago", "Today 3:42 PM", "Yesterday 9:10 AM"), and a
message that comes more than five minutes after the last one starts a new batch
with its own stamp. Tap a message to see exactly when it was sent — and, on your
own (or any, for a moderator), the delete link.

Tune it from your stylesheet. The colours and height are custom properties on
the tag:

```css
friendo-chat {
  --chat-mine: #6d5dfc;      /* your bubbles and the Send button */
  --chat-theirs: #e9e9eb;    /* everyone else's bubbles */
  --chat-border: #d9d9de;    /* the box, the composer's line, the input */
  --chat-background: #fff;   /* the box */
  --chat-height: 60vh;       /* the list, before it scrolls */
}
```

For anything else, style the parts with `::part()`: `chat` (the box), `list`,
`message` (and `mine` on your own), `author`, `body` (the bubble), `meta` (the
line a tap shows), `sent` (its exact time), `time` (a batch's stamp), `delete`, `form`, `input`, `submit`, `status`, `empty`, `signed-out`
and `error`.

## One chat per post

Because the chat is made from whatever the rendered page names, a template can
name one per record — a live thread under every event, say:

```html
<friendo-chat chat-id="event-{{ record.slug }}"></friendo-chat>
```

Each event's page makes its own chat the first time someone opens it. A chat id
is a slug: letters, digits, dots, dashes and underscores, up to 64 characters.
An id that doesn't fit is ignored rather than registered.

Only what your own templates render counts, so a visitor can't make chats by
posting a comment — unless a template renders their text unescaped (`|safe`),
which it shouldn't.

## The API

`GET /_/api/chats` lists the site's chats. A chat's messages are readable as
plain JSON at `GET /_/api/chats/<id>/messages`, which is what the tag fetches
before it opens the live stream at `/_/api/chats/<id>/stream`.

An owner or admin can also make a chat ahead of time, or give it a display
name, with `POST /_/api/chats` and `{"id": "general", "name": "General"}`
(`id` optional — one is generated; `name` defaults to the id). `DELETE
/_/api/chats/<id>` removes a chat and its messages.

## Limits and moderation

Posting is rate-limited per member (the same limit as comments). An owner, admin
or editor can delete any message; members delete only their own. A deleted message
disappears from every open page, the same way a new one arrives. There's no
server-rendered form of a chat — a chat is inherently live, so it's the one
community feature that's a component only.
