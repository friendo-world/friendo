---
title: Roles and permissions
slug: roles
section: Reference
weight: 80
description: What each role can do on a site, the preset settings that shape it, and the roles inside a group.
---

Everyone who signs in is a **member**. A **role** adds to what a member can do, and
each role includes everything below it. A **visitor** is someone not signed in. Why
it works this way is in [Signing in & roles](/docs/signing-in).

## Site roles

Highest first:

| Role | Adds |
|---|---|
| **Owner** | Makes and removes admins and other owners; hands the site on |
| **Admin** | Settings, members, pushes and pulls. An admin of every group |
| **Editor** | Writes and edits any post, and publishes |
| **Moderator** | Approves and rejects comments and posts waiting for review, without editing them. A moderator of every group |
| **Contributor** | Writes and edits their own posts; reviews comments on them |
| member | Comments, reacts, votes, RSVPs, follows, joins groups |

## What each role can do

✓ can · – can't · *setting* depends on a [preset setting](#presets)

| Action | Visitor | Member | Contributor | Moderator | Editor | Admin | Owner |
|---|---|---|---|---|---|---|---|
| Read public pages | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Comment | – | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| React | – | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Vote in a poll | – | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| RSVP | – | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Follow a profile | – | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Join a group | – | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Start a group | – | *`members_can_start_groups`* | ✓ | ✓ | ✓ | ✓ | ✓ |
| Post from a `<friendo-form>` | – | *`members_can_post`*, always waits for review | ✓ | ✓ | ✓ | ✓ | ✓ |
| Write and edit own posts | – | – | ✓ | ✓ | ✓ | ✓ | ✓ |
| Own posts go live without review | – | – | *unless `posts_need_review`* | *unless `posts_need_review`* | ✓ | ✓ | ✓ |
| Edit any post | – | – | – | – | ✓ | ✓ | ✓ |
| Publish (set any status) | – | – | – | – | ✓ | ✓ | ✓ |
| Approve or reject comments | – | – | on own posts | ✓ | ✓ | ✓ | ✓ |
| Approve or reject posts waiting for review | – | – | – | ✓ | ✓ | ✓ | ✓ |
| Manage members (add, remove, set roles up to editor) | – | – | – | – | – | ✓ | ✓ |
| Change settings, push and pull | – | – | – | – | – | ✓ | ✓ |
| Make admins and owners | – | – | – | – | – | – | ✓ |
| Hand the site on (make another owner) | – | – | – | – | – | – | ✓ |

Features switched off in **Settings → Features** (comments, reactions, polls, RSVP,
follows, groups…) are off for every role.

## Rules on managing members

| Rule | |
|---|---|
| Rank | An admin can't change or remove anyone at or above admin. Owners can manage other owners |
| Last owner | A site always keeps one owner. The last owner can't be demoted or removed: make someone else an owner first, then step down |
| Yourself | No one can delete their own account from **Members** |
| New sign-ups | Start as `member`, or `contributor` with `signups_are_contributors` |
| Deleting a site | An operator's job on the network, not a role |

## Presets

In admin **Settings → Members & roles**, a preset sets three keys and who sees
profiles at once:

| Preset | `open_signups` | `signups_are_contributors` | `posts_need_review` | `profile_visibility` |
|---|---|---|---|---|
| **Personal** | `false` | `false` | `false` | `members` |
| **Community** | `true` | `true` | `false` | `public` |
| **Blog** | `true` | `false` | `true` | `public` |

## Settings keys

Each is a key in `[settings]` of [friendo.toml](/docs/config) and a switch in the
admin. A key written in friendo.toml is locked in the admin.

| Key | What | Default |
|---|---|---|
| `open_signups` | Anyone can sign up. Off: only people an admin adds have accounts | `true` |
| `signups_are_contributors` | New members start as contributors | `false` |
| `posts_need_review` | Contributors' and moderators' posts wait for review | `false` |
| `members_can_post` | Members can post from a `<friendo-form>`; their posts always wait for review | `false` |
| `comments_need_review` | Comments wait for review | `true` |
| `members_can_start_groups` | Any member can start a group and is its admin | `false` |
| `password_login` | Allow signing in with a password as well as a code | `false` |
| `profile_visibility` | `members` or `public`: who can see profiles | `members` |

```toml
[settings]
open_signups = true
signups_are_contributors = true
comments_need_review = false
```

## Page gates

A tag at the top of a page, or a path list in `[access]`, keeps a page for one role
and up. See [Members-only pages](/docs/members-only).

| Tag | `[access]` key | Who sees the page |
|---|---|---|
| `{% members only %}` | `members_only` | anyone signed in |
| `{% contributors only %}` | `contributors_only` | contributor and up |
| `{% moderators only %}` | `moderators_only` | moderator and up |
| `{% editors only %}` | `editors_only` | editor and up |
| `{% admins only %}` | `admins_only` | admin and up |
| `{% owners only %}` | `owners_only` | owners |

## Group roles

A [group](/docs/groups) has its own three roles:

| Role | Can |
|---|---|
| **Admin** | Change `visibility` and `join`, add and remove people, make admins and moderators, manage the group's chats |
| **Moderator** | Approve requests to join, remove members, delete messages |
| **Member** | Read the group, post in its chats |

| Rule | |
|---|---|
| First admin | Whoever makes a group. A group from a `content/groups/*.md` file starts with none until a site admin makes one |
| Site admins and owners | Admins of every group |
| Site moderators and editors | Moderators of every group |
| Last admin | A group always keeps one admin; the last one can't leave |
