---
title: API
slug: api
section: Reference
weight: 11
---

Every site serves its API under **`/_/api`**. The admin, the `<friendo-*>` tags
and the `friendo` command all use it; you can too. Requests and responses are
JSON. Sign in with the `friendo_session` cookie (what the tags and the admin
hold) and the API knows who you are.

Two words recur. A **post** is `{id, collection, slug, title, body, status,
author_id, published_at, created, updated, fields, when, location}`; a **profile**
is `{id, name, slug, avatar, bio, url, fields}`. Every id you pass is a post's or a
profile's.

## Signing in

| Route | What |
|---|---|
| `POST /auth/request-code {email}` | Send a sign-in code (echoed back in local dev) |
| `POST /auth/verify-code {email, code}` | Sign in; makes a member account if there isn't one |
| `POST /auth/login {email, password}` | Sign in with a password, where the site allows them |
| `POST /auth/logout` | Sign out |
| `GET /me` | Who's signed in: `user` with `role`, `profile_id`, `following`, `groups`, `unread` |
| `GET /features` | Which features are on |

## Posts

| Route | Who |
|---|---|
| `GET /collections` | The site's collections and their fields | contributors and up |
| `GET /collections/{collection}/posts` | The posts in one | contributors and up |
| `POST /collections/{collection}/posts {title, body, slug?, status?, fields?}` | Make a post. `fields.when` (a string or `{start, end, repeats, …}`) makes it an event; a `{lat, lng}` field gives it a location | contributors and up, or members when *Members can post* is on (then it waits for review) |
| `GET /posts/{id}` | One post | contributors and up |
| `PUT /posts/{id}` | Edit a post. A `fields` without `when` leaves the event alone; `"when": null` removes it | its author, or editors and up |
| `PUT /posts/{id}/status {status}` | Approve (`published`) or reject (`draft`) a post waiting for review; editors may set any status | moderators and up |
| `DELETE /posts/{id}` | Delete a post | its author, or editors and up |
| `GET /posts?status=pending` | The review queue | moderators and up |

Responses wrap the post: `{"post": {…}}`, or `{"posts": [...]}` for lists.

## Comments

| Route | Who |
|---|---|
| `GET /posts/{id}/comments` | The approved comments | public |
| `POST /posts/{id}/comments {body}` | Write one (it waits for review unless the site turned that off) | members |
| `GET /comments?status=pending` | Comments to review | the post's author, or moderators and up |
| `PUT /comments/{id} {status}` | Approve or reject | the post's author, or moderators and up |
| `DELETE /comments/{id}` | Delete | its writer, the post's author, or moderators and up |

## Reactions and polls

| Route | Who |
|---|---|
| `GET /reactions?post_id=` (or `comment_id=`) | Per-emoji counts, and whether you reacted | public |
| `POST /reactions {post_id \| comment_id, emoji}` | Toggle yours | members |
| `GET /polls/by-slug/{slug}`, `GET /polls/{id}` | A poll and its tally | public |
| `POST /polls/{id}/vote {option}` | Vote once | members |
| `POST /polls {post_id, question, options}` | Make a poll without front matter | editors and up |

## Events and RSVPs

The calendar feeds live at the **site root**, not under `/_/api`:
`/calendar.ics` and `/calendar.json`, with `?collection=`, `?group=`, `?post=`,
`?date=`, `?from=&to=`, `?mine=1` and `?token=`. See [Calendar](/docs/calendar).

| Route | Who |
|---|---|
| `GET /posts/{id}/rsvps?date=` | The tally for a date (the next one by default), your answer, and the names for organizers | public |
| `POST /posts/{id}/rsvps {answer, date?}` | Answer `going`, `maybe` or `not_going` | members |
| `DELETE /posts/{id}/rsvps?date=` | Take your answer back | members |
| `GET /posts/{id}/rsvps/names` (`?format=csv`) | Every answer, grouped by date | the author, or moderators and up |
| `POST /posts/{id}/invites {slugs?, group?, followers?, date?}` | Invite people | the author, or moderators and up |
| `GET /me/calendar`, `POST /me/calendar/reset` | The member's private feed link, and a fresh one | members |

## Locations and files

| Route | Who |
|---|---|
| `GET /locations?post_id=` | A post's locations; without `post_id`, every published post's (`?bbox=` to narrow) | public |
| `POST /locations {post_id, lat, lng, label}` | Give a post a location | its author, or editors and up |
| `DELETE /locations/{id}` | Remove one | its author, or editors and up |
| `GET /files?post_id=` | A post's images | public |
| `POST /files` (multipart: `post_id`, `field`, `file`) | Upload an image (10 MB, images only). Returns its public `url` | contributors and up |
| `DELETE /files/{id}` | Remove one | contributors and up |

## Chats

| Route | Who |
|---|---|
| `GET /chats` | The site's chats | public |
| `GET /chats/{id}/messages` | Messages, plus `can_post` and `can_moderate` for you | public |
| `GET /chats/{id}/stream` | New messages, live (server-sent events) | public |
| `POST /chats/{id}/messages {body}` | Post one | members |
| `DELETE /messages/{id}` | Delete one | its writer, or moderators and up |
| `POST /chats {id?, name?}`, `DELETE /chats/{id}` | Make or remove a chat by hand | admins and up |
| `GET /groups/{id}/chats` | A group's chats | its members |
| `POST /groups/{id}/chats {id?, name}`, `DELETE /groups/{id}/chats/{chat}` | Add or remove one | the group's admins |
| `GET/POST /groups/{id}/chats/{chat}/messages`, `GET …/stream` | As above, for the group's members | its members |

## Profiles and follows

| Route | Who |
|---|---|
| `GET /me/profiles` | Your profiles (`is_default` marks the current one) | members |
| `POST /me/profiles {name}` | Add one | members |
| `PUT /me/profiles/{id} {name?, slug?, avatar?, bio?, fields?}` | Edit one | members |
| `POST /me/profiles/{id}/default` | Make it your current one | members |
| `DELETE /me/profiles/{id}` | Remove one (never the last) | members |
| `GET /profiles`, `GET /profiles/{slug}` | Everyone's profile pages, as JSON | as `profile_visibility` says |
| `GET /follows?profile_id=` (or `slug=`) | Follower count, and whether you follow them | public |
| `POST /follows {profile_id}` | Toggle | members |
| `DELETE /follows/{profile_id}` | Unfollow | members |
| `GET /profiles/{slug}/followers`, `…/following` | Who | public |
| `GET /me/notifications` (`?unread=1`, `?limit=`) | Your inbox; each row has `kind`, `from` (a profile) and a `target` | members |
| `PUT /me/notifications/{id}/read`, `PUT /me/notifications/read-all` | Mark read | members |

## Groups

| Route | Who |
|---|---|
| `GET /groups` | The groups you may see, with your standing and `can_create` | public |
| `GET /groups/{id}` | One group (by id or slug): members, and requests for moderators | as its visibility says |
| `POST /groups/{id}/join`, `DELETE /groups/{id}/join` | Join (or ask), and leave | members |
| `POST /groups/{id}/members {slug \| profile_id, role?}` | Add someone | the group's admins |
| `PUT /groups/{id}/members/{profile_id} {status \| role}` | Approve a request (`status: member`); change a role (`admin`, `moderator`, `member`) | moderators approve; admins change roles |
| `DELETE /groups/{id}/members/{profile_id}` | Remove, or decline a request | the group's moderators |
| `PUT /groups/{id}/settings {visibility?, join?}` | Change the two settings | the group's admins |

A group is made like any post: `POST /collections/groups/posts`.

## Members and settings

| Route | Who |
|---|---|
| `GET /users`, `POST /users {email, name, role, password?}`, `PUT /users/{id}`, `DELETE /users/{id}` | The site's members | admins and up (owner and admin roles need an owner) |
| `GET /settings`, `PUT /settings {…}` | Every [setting](/docs/config#settings) by its one name, plus `features` | admins and up |
| `GET /content/toml` | The `[content]` block that matches the site | admins and up |
| `POST /push/…`, `GET /pull/…` | What `friendo push` and `pull` use | admins and up |

## Turning features off

A feature that's off answers `403` with `off: true` to every route it owns, and
`GET /features` says which are on.
