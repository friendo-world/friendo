---
title: Words friendo uses
slug: words
section: Reference
weight: 100
description: One word for each idea. The whole dictionary on one page.
---

Friendo tries to use one word for each idea, and the same word everywhere: in
friendo.toml, in templates, in the admin, on the command line and in these docs.
This page is the whole dictionary. If a word isn't here, it isn't a friendo word.

## Where things live

| Word | Meaning |
|---|---|
| **site** | A folder: templates, pages, assets and a database. See [A site is a folder](/docs/site-is-a-folder). |
| **page** | A file in `pages/`. Its path is its URL: `pages/about.html` is `/about`. |
| **layout** | A file in `layouts/` that pages extend. |
| **collection** | A folder of posts: `blog`, `events`, `groups`, `pages`. The folder under `content/` and the folder under `pages/` share its name. |
| **post** | One thing in a collection. Every post has a title, a slug, a body and a status; anything else is a field. |
| **anonymous** | A post or comment whose member chose not to show their name. It says "Anonymous"; moderators and editors still see who. (Nobody's name at all is a **drop box**.) |
| **drop box** | A collection anyone can post to, where no one's name is kept: `<friendo-form collection="tips" drop-box>`. See [Drop boxes](/docs/visitors#drop-boxes). |
| **field** | An extra key on a post: from a content file's front matter, a form, or the admin. `{{ post.fields.mood }}`. |
| **feature** | A switch in Settings → Features: comments, reactions, polls, chats, locations, RSVPs, follows, groups. Off, the feature disappears from the site. |
| **network** | One friendo hosting many sites by subdomain. friendo.world is a network; you can run your own. |
| **operator** | Someone who runs a network. |

## People

| Word | Meaning |
|---|---|
| **visitor** | Someone who isn't signed in. When a site allows it, a visitor can react, vote, RSVP, comment or post; their browser remembers what they did until they sign in, and then it's theirs. |
| **member** | Anyone with an account on your site. Signing in with a code makes you one. |
| **role** | Extra powers a member may have: contributor, moderator, editor, admin, owner. A member with no role can comment, react, vote and RSVP. |
| **admin** | Runs the thing: settings, people, deleting. A site has admins; so does a group. |
| **moderator** | Keeps the thing tidy: approves and rejects, removes, deletes messages, but doesn't edit what others wrote. A site has moderators; so does a group. |
| **profile** | A member's public face: name, avatar, bio, a page at `/profiles/<slug>`. One account may have several. |
| **author** | The profile a post is by: `{{ post.author.name }}`. |
| **account** | The sign-in identity behind a member, an email address. You'll rarely need the word. |

**A site is one big group.** Both use the same words: admins run it, moderators keep
it tidy, everyone else is a member. Site admins are admins of every group; site
moderators moderate every group.

## Doing things

| Word | Meaning |
|---|---|
| **sign in** | Enter your email, get a code, enter the code. (The page and command are spelled `login`.) |
| **review** | Where things wait for a human: comments and posts wait there until a moderator **approves** them (a comment can also be **rejected**). Requests to join a group wait in the group itself. See [Review comments and posts](/docs/review). |
| **publish** | Make a post live. A post is a **draft** until then. |
| **deploy** | Put a site on a network for the first time. After that, **push** changes up and **pull** changes down. |
| **export** | Write the site as plain HTML files. |
| **import** | Read the `content/` folder into the site's database. `friendo serve` does it for you. |

## Time and place

| Word | Meaning |
|---|---|
| **event** | A post with a `when`. It gets a date, a calendar feed and RSVPs. |
| **when** | An event's time: `when: 2026-10-04 19:00 to 21:00`, or a map with `start`, `end`, `repeats`, `except`, `timezone`. |
| **date** | One time a repeating event happens. |
| **location** | A place on a post: `location: 47.6, -122.3`. `<friendo-map>` draws it. |

## Pointing at things

| Word | Meaning |
|---|---|
| **id** | A post's stable id, `{{ post.id }}`. Every tag that takes a post takes `post-id`. |
| **slug** | The word in a URL: `/blog/hello-world`, `/profiles/pat`, `/groups/board`. Chats have one too: `chat-id="general"`. |

## Words friendo doesn't use

record, persona, content type, data (for a post's fields), attendee, occurrence,
pin, where, tenant, capability. If you find one in the docs, it's a bug.
