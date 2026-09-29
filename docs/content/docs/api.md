---
title: API
slug: api
section: Reference
weight: 70
description: The site's own HTTP API, which the tags and the CLI use.
---

Every site serves its API under **`/_/api`**. The admin, the `<friendo-*>` tags
and the `friendo` command all use it; you can too. Requests and responses are
JSON. Sign in with the `friendo_session` cookie (what the tags and the admin
hold) and the API knows who you are. The examples below sign in once with
`curl -c cookies.txt`, then send the cookie back with `-b cookies.txt`.

Two words recur. A **post** is `{id, collection, slug, title, body, status,
author_id, published_at, created, updated, fields, when}`; a **profile** is
`{id, name, slug, avatar, bio, role, url, fields, created}`. Every id you pass is
a post's or a profile's. A post's locations come from `GET /locations`.

An error answers with a status code and `{"error": "…"}`: `401` when you aren't
signed in, `403` when your role can't do it.

## Signing in

| Route | What |
|---|---|
| `POST /auth/request-code {email}` | Send a sign-in code (echoed back in local dev). Makes a member account for a new email, when the site allows sign-ups |
| `POST /auth/verify-code {email, code}` | Sign in with the code; sets the `friendo_session` cookie |
| `POST /auth/login {email, password}` | Sign in with a password, where the site allows them |
| `POST /auth/logout` | Sign out |
| `GET /me` | Who's signed in: `user` with `role`, `profile_id`, `following`, `groups`, `unread` |
| `GET /features` | Which features are on |

The session lasts seven days.

```bash tab="Request" group=api
# 1. Ask for a code
curl -X POST https://my-site.friendo.world/_/api/auth/request-code \
  -H 'Content-Type: application/json' \
  -d '{"email": "sam@example.com"}'

# 2. Trade the code for a session; -c saves the cookie
curl -X POST https://my-site.friendo.world/_/api/auth/verify-code \
  -H 'Content-Type: application/json' \
  -c cookies.txt \
  -d '{"email": "sam@example.com", "code": "483920"}'
```

```json tab="Response" group=api
{
  "user": {
    "id": "9f2c41d07a6b3e58c1d04a7e",
    "email": "sam@example.com",
    "name": "",
    "role": "member",
    "created": "2026-09-29T14:02:11Z"
  }
}
```

## Posts

| Route | What | Who |
|---|---|---|
| `GET /collections` | The site's collections and their fields | contributors and up |
| `GET /collections/{collection}/posts` | The posts in one | contributors and up |
| `POST /collections/{collection}/posts {title, body, slug?, status?, fields?}` | Make a post. `fields.when` (a string or `{start, end, repeats, …}`) makes it an event; a `{lat, lng}` field gives it a location | contributors and up, or members when *Members can post* is on (then it waits for review) |
| `GET /posts/{id}` | One post | its author (a contributor or up), or editors and up |
| `PUT /posts/{id}` | Edit a post. A `fields` without `when` leaves the event alone; `"when": null` removes it | its author (a contributor or up), or editors and up |
| `PUT /posts/{id}/status {status}` | Approve (`published`) or reject (`draft`) a post waiting for review; editors may set any status | moderators and up |
| `DELETE /posts/{id}` | Delete a post | its author (a contributor or up), editors and up, or moderators for a post waiting for review |
| `GET /posts?status=pending` | The review queue (`pending` is the default) | moderators and up |

Responses wrap the post: `{"post": {…}}`, or `{"posts": [...]}` for lists. A new
post answers `201`.

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/collections/blog/posts \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"title": "Hello", "body": "First post from the API.", "slug": "hello", "fields": {"summary": "A first post"}}'
```

```json tab="Response" group=api
{
  "post": {
    "id": "3b7e90a1c24f5d6e8a0b1c2d",
    "collection": "blog",
    "slug": "hello",
    "title": "Hello",
    "body": "First post from the API.",
    "author_id": "a41f0c9e27b83d5610e4f2ab",
    "status": "published",
    "published_at": "",
    "created": "2026-09-29T14:05:40Z",
    "updated": "2026-09-29T14:05:40Z",
    "fields": { "summary": "A first post" },
    "when": null
  }
}
```

## Comments

| Route | What | Who |
|---|---|---|
| `GET /posts/{id}/comments` | The approved comments, plus `can_moderate` for you | public |
| `POST /posts/{id}/comments {body, parent_id?}` | Write one (it waits for review unless the site turned that off) | members |
| `GET /comments?status=pending` | Comments to review (`pending` is the default) | contributors and up for their own posts; moderators and up for every post |
| `PUT /comments/{id} {status}` | Set `approved`, `rejected` or `pending` | the post's author, or moderators and up |
| `DELETE /comments/{id}` | Delete | its writer, the post's author, or moderators and up |

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/posts/3b7e90a1c24f5d6e8a0b1c2d/comments \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"body": "Lovely write-up."}'
```

```json tab="Response" group=api
{
  "comment": {
    "id": "c0d9e8f7a6b5c4d3e2f1a0b9",
    "post_id": "3b7e90a1c24f5d6e8a0b1c2d",
    "parent_id": "",
    "author_id": "a41f0c9e27b83d5610e4f2ab",
    "author_name": "Sam",
    "author_avatar": "",
    "body": "Lovely write-up.",
    "status": "pending",
    "created": "2026-09-29T14:08:02Z"
  }
}
```

## Reactions and polls

| Route | What | Who |
|---|---|---|
| `GET /reactions?post_id=` (or `comment_id=`) | Per-emoji counts, and whether you reacted | public |
| `POST /reactions {post_id \| comment_id, emoji}` | Toggle yours | members |
| `GET /polls/by-slug/{slug}`, `GET /polls/{id}` | A poll and its tally | public |
| `POST /polls/{id}/vote {option_index}` | Vote once, by the option's number (from 0) | members |
| `POST /polls {post_id, question, options, closes_at?}` | Make a poll without front matter | editors and up |

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/polls/5e6f7a8b9c0d1e2f3a4b5c6d/vote \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"option_index": 1}'
```

```json tab="Response" group=api
{
  "poll": {
    "id": "5e6f7a8b9c0d1e2f3a4b5c6d",
    "slug": "next-meetup",
    "question": "Where should we meet next?",
    "options": [
      { "index": 0, "text": "The library", "votes": 3 },
      { "index": 1, "text": "The park", "votes": 5 }
    ],
    "total_votes": 8,
    "closes_at": "",
    "my_vote": 1
  }
}
```

## Events and RSVPs

The calendar feeds live at the **site root**, not under `/_/api`:
`/calendar.ics` and `/calendar.json`, with `?collection=`, `?group=`, `?post=`,
`?date=`, `?from=&to=`, `?mine=1` and `?token=`. See [Calendar](/docs/calendar).

| Route | What | Who |
|---|---|---|
| `GET /posts/{id}/rsvps?date=` | The tally for a date (the next one by default), your answer, and the names for organizers | public |
| `POST /posts/{id}/rsvps {answer, date?}` | Answer `going`, `maybe` or `not_going` | members |
| `DELETE /posts/{id}/rsvps?date=` | Take your answer back | members |
| `GET /posts/{id}/rsvps/names` (`?format=csv`) | Every answer, grouped by date | the author, or moderators and up |
| `POST /posts/{id}/invites {slugs?, profile_ids?, group?, followers?, date?}` | Invite people | the author, or moderators and up |
| `GET /me/calendar`, `POST /me/calendar/reset` | The member's private feed link, and a fresh one | members |

A `date` is the RFC 3339 start of one date of the event.

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/posts/7c8d9e0f1a2b3c4d5e6f7a8b/rsvps \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"answer": "going"}'
```

```json tab="Response" group=api
{
  "date": "2026-10-03T19:00:00-04:00",
  "date_text": "Sat Oct 3, 7 pm",
  "counts": { "going": 12, "not_going": 2, "maybe": 4, "invited": 3 },
  "mine": "going"
}
```

## Locations and files

| Route | What | Who |
|---|---|---|
| `GET /locations?post_id=` | A post's locations; without `post_id`, every published post's (`?bbox=` to narrow) | public |
| `POST /locations {post_id, lat, lng, label}` | Give a post a location | its author (a contributor or up), or editors and up |
| `DELETE /locations/{id}` | Remove one | its author (a contributor or up), or editors and up |
| `GET /files?post_id=` | A post's images (`post_id` is required) | public |
| `POST /files` (multipart: `post_id`, `field`, `file`) | Upload an image (10 MB, images only). Returns its public `url` | contributors and up |
| `DELETE /files/{id}` | Remove one | contributors and up |

`bbox` is `west,south,east,north`: `minLng,minLat,maxLng,maxLat`.

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/locations \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"post_id": "3b7e90a1c24f5d6e8a0b1c2d", "lat": 45.5231, "lng": -122.6765, "label": "Pioneer Square"}'
```

```json tab="Response" group=api
{
  "location": {
    "id": "e1f2a3b4c5d6e7f8a9b0c1d2",
    "target_type": "post",
    "target_id": "3b7e90a1c24f5d6e8a0b1c2d",
    "lat": 45.5231,
    "lng": -122.6765,
    "label": "Pioneer Square",
    "created": "2026-09-29T14:12:30Z"
  }
}
```

## Chats

| Route | What | Who |
|---|---|---|
| `GET /chats` | The site's chats | public |
| `GET /chats/{id}/messages` | Messages, plus `can_post` and `can_moderate` for you | public |
| `GET /chats/{id}/stream` | New messages, live (server-sent events) | public |
| `POST /chats/{id}/messages {body}` | Post one | members |
| `DELETE /messages/{id}` | Delete one | its writer, or moderators and up |
| `POST /chats {id?, name?}`, `DELETE /chats/{id}` | Make or remove a chat by hand | admins and up |
| `GET /groups/{id}/chats` | A group's chats | whoever may see the group |
| `POST /groups/{id}/chats {id?, name}`, `DELETE /groups/{id}/chats/{chat}` | Add or remove one | the group's admins |
| `GET/POST /groups/{id}/chats/{chat}/messages`, `GET …/stream` | As above, for the group's members | its members |

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/chats/general/messages \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"body": "Anyone up for Saturday?"}'
```

```json tab="Response" group=api
{
  "message": {
    "id": "f0e1d2c3b4a5968778695a4b",
    "chat_id": "general",
    "parent_id": "",
    "author_id": "a41f0c9e27b83d5610e4f2ab",
    "body": "Anyone up for Saturday?",
    "created": "2026-09-29T14:15:07Z",
    "author_name": "Sam",
    "author_avatar": ""
  }
}
```

## Profiles and follows

| Route | What | Who |
|---|---|---|
| `GET /me/profiles` | Your profiles (`is_default` marks the current one) | members |
| `POST /me/profiles {name}` | Add one | members |
| `PUT /me/profiles/{id} {name?, slug?, avatar?, bio?, fields?}` | Edit one | members |
| `POST /me/profiles/{id}/default` | Make it your current one | members |
| `DELETE /me/profiles/{id}` | Remove one (never the last) | members |
| `GET /profiles`, `GET /profiles/{slug}` | Everyone's profile pages, as JSON | as `profile_visibility` says |
| `GET /follows?profile_id=` (or `slug=`) | Follower count, and whether you follow them | public |
| `POST /follows {profile_id \| slug}` | Toggle | members |
| `DELETE /follows/{profile_id}` | Unfollow | members |
| `GET /profiles/{slug}/followers`, `…/following` | Who | as `profile_visibility` says |
| `GET /me/notifications` (`?unread=1`, `?limit=`) | Your inbox; each row has `kind`, `from` (a profile) and a `target` | members |
| `PUT /me/notifications/{id}/read`, `PUT /me/notifications/read-all` | Mark read | members |

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/follows \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"slug": "robin"}'
```

```json tab="Response" group=api
{
  "profile_id": "b52e1d0a38f94c6721f5e3bc",
  "following": true,
  "followers": 18,
  "following_count": 7
}
```

## Groups

| Route | What | Who |
|---|---|---|
| `GET /groups` | The groups you may see, with your standing and `can_create` | public |
| `GET /groups/{id}` | One group (by id or slug): members, and requests for moderators | as its visibility says |
| `GET /groups/{id}/members` (`?status=`) | The members; `status=requested` or `invited` for moderators | as its visibility says |
| `POST /groups/{id}/join`, `DELETE /groups/{id}/join` | Join (or ask), and leave | members |
| `POST /groups/{id}/members {slug \| profile_id, role?}` | Add someone | the group's admins |
| `PUT /groups/{id}/members/{profile_id} {status \| role}` | Approve a request (`status: member`); change a role (`admin`, `moderator`, `member`) | moderators approve; admins change roles |
| `DELETE /groups/{id}/members/{profile_id}` | Remove, or decline a request | the group's moderators |
| `PUT /groups/{id}/settings {visibility?, join?}` | Change the two settings | the group's admins |

A group is made like any post: `POST /collections/groups/posts`.

```bash tab="Request" group=api
curl -X POST https://my-site.friendo.world/_/api/groups/book-club/join \
  -b cookies.txt
```

```json tab="Response" group=api
{
  "group": {
    "id": "d4c3b2a1f0e9d8c7b6a59483",
    "slug": "book-club",
    "title": "Book club",
    "body": "We read one book a month.",
    "author_id": "b52e1d0a38f94c6721f5e3bc",
    "fields": { "visibility": "public", "join": "open" },
    "created": "2026-09-01T10:00:00Z",
    "updated": "2026-09-01T10:00:00Z",
    "settings": { "visibility": "public", "join": "open" },
    "member_count": 9,
    "chats": [],
    "url": "/groups/book-club",
    "mine": { "role": "member", "status": "member" },
    "moderates": false,
    "administers": false
  }
}
```

## Members and settings

| Route | What | Who |
|---|---|---|
| `GET /users`, `POST /users {email, name, role, password?}`, `PUT /users/{id}`, `DELETE /users/{id}` | The site's members | admins and up (owner and admin roles need an owner) |
| `GET /settings`, `PUT /settings {…}` | Every [setting](/docs/config#settings) by its one name, plus `features` | admins and up |
| `GET /content/toml` | The `[content]` block that matches the site | admins and up |
| `POST /push/…`, `GET /pull/…` | What `friendo push` and `pull` use | admins and up |

```bash tab="Request" group=api
curl https://my-site.friendo.world/_/api/users \
  -b cookies.txt
```

```json tab="Response" group=api
{
  "users": [
    {
      "id": "1a2b3c4d5e6f7a8b9c0d1e2f",
      "email": "owner@example.com",
      "name": "Alex",
      "role": "owner",
      "created": "2026-08-12T09:30:00Z",
      "profile_slug": "alex"
    },
    {
      "id": "9f2c41d07a6b3e58c1d04a7e",
      "email": "sam@example.com",
      "name": "Sam",
      "role": "member",
      "created": "2026-09-29T14:02:11Z",
      "profile_slug": "sam"
    }
  ]
}
```

## Turning features off

A feature that's off answers `403` with `off: true` to every route it owns, and
`GET /features` says which are on. The features are `comments`, `reactions`,
`polls`, `rsvp`, `locations`, `chats`, `follows` and `groups`.

```bash tab="Request" group=api
curl https://my-site.friendo.world/_/api/features
```

```json tab="Response" group=api
{
  "features": {
    "comments": true,
    "reactions": true,
    "polls": true,
    "rsvp": true,
    "locations": false,
    "chats": true,
    "follows": true,
    "groups": true
  }
}
```
