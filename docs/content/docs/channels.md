---
title: Channels (realtime chat)
slug: channels
group: Concepts
weight: 9
---

A **channel** is a realtime message feed — a chat room, a comments-style
stream, a live thread under an event. Signed-in members post; new messages
stream in live over a single server-sent-events connection, no polling.

## Put one on a page

```html
<friendo-auth></friendo-auth>
<friendo-channel channel-id="general"></friendo-channel>
<script src="/friendo.js" defer></script>
```

That's the whole feature. Signed-out visitors see the messages and a "Sign in
to join" line; signed-in members get a composer, and can delete their own
messages. Style it from your stylesheet with `::part()` — the parts are `list`,
`message`, `author`, `body`, `form`, `input`, `submit`, `status`, `delete`,
`empty` and `error`.

## Making a channel

Channels are created by an owner or admin (it's a site-configuration act). In
the admin there isn't a screen for it yet, so use the API once — from the
admin's own session cookie, or `curl` with a signed-in session:

```bash
curl -X POST https://my-site.friendo.world/_/api/channels \
  -H 'Content-Type: application/json' -b 'friendo_session=…' \
  -d '{"name": "general"}'
```

The response carries the channel's `id`; that's what `channel-id` takes.
`GET /_/api/channels` lists them. A channel's messages are also readable as
plain JSON at `GET /_/api/channels/<id>/messages`, which is what the tag fetches
before it opens the live stream at `/_/api/channels/<id>/stream`.

## Limits and moderation

Posting is rate-limited per member (the same limit as comments). An owner, admin
or editor can delete any message; members delete only their own. There's no
server-rendered form of a channel — a chat is inherently live, so it's the one
community feature that's a component only.
