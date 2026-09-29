---
title: The social graph
slug: social-graph
section: Concepts
weight: 80
description: How friendo connects the people on a site with profiles, follows, groups and an inbox, and why each piece is shaped the way it is.
---

A friendo site can know who its members are to each other: who follows whom, who
is in which group, and what happened to them while they were away. All of it lives
in the site's own database, next to the posts. This page explains the model; the
how-to is in [Add profiles and follows](/docs/profiles) and [Groups](/docs/groups).

## An account and a profile

An **account** is who signs in: an email address and, if the site allows it, a
password. A **profile** is what other people see: a name, an avatar, a bio, any
[declared fields](/docs/profiles#3-add-fields-to-profiles) and a page at
`/profiles/<slug>`. Every account gets a profile when it's made, and may keep more
than one, a real name and a pen name, say. It picks which one its posts, comments
and messages are by.

Everything a person writes points at the profile, not the account. That's what
lets one person write as two names, and it's why a profile never shows an email.
It's also why `post.author` is a profile, and why following, joining a group and
answering an RSVP are all things a profile does.

Profiles are not posts. Making a person a post would drag a status, a review queue
and publishing onto people, none of which fits. So profiles have their own page,
`pages/profiles/[slug].html`, and their own lever: `profile_visibility`, which
keeps them for members (the default) or shows them to everyone. There's no switch
to turn profiles off; every member has one.

## Follows are one way

A **follow** is one profile subscribing to another. It needs no permission and
sends nothing back: you follow Pat, and Pat's posts can show up in your feed
through the `by_following` filter.

A **friend** is two people who follow each other. Friendo works that out from the
follows; it's never stored and never requested. There is no "accept" step, because
an approval flow needs somewhere to tell people about the request and a place to
answer it, and one-way follows are one idea instead of two.

A template sees both sides without JavaScript. The viewer carries
`user.following`, `user.followers` and `user.friends`; a profile page carries
`profile.followers` and `profile.following` with their counts. `<friendo-follow>`
is the button that changes it.

## Groups are posts with members

A **group** is a post in the `groups` collection, the way an event is a post with a
`when`. It has a title, a slug, a body and a page like any post, and friendo keeps
a list of its members beside it. Its fields say who can see it (`visibility`) and
how people join (`join`).

A group mirrors the site. Its **admins** run it, its **moderators** keep it tidy,
and everyone else is a member. Site admins are admins of every group and site
moderators moderate every group, so the same words mean the same things at both
sizes.

Because a group is a post, filing something under it is only naming it:
`group: board` on a post or an event. Nothing new to set up, and the calendar, the
filters and the members-only tag all read the same `group` value. The viewer's
groups arrive as `user.groups`, which is why
`{% members only if "board" in user.groups %}` needed no new syntax.

## The inbox is the only channel

Every social feature needs a way to say "this happened". In friendo that's the
**inbox**: a list of notes a member sees when they visit, through `<friendo-inbox>`,
the badge on `<friendo-signin>`, or `{{ user.unread }}`. A note is written when
someone follows you, when a comment on your post is approved, when someone asks to
join a group you moderate, when you're added to a group or let in, and when someone
invites you to an event.

Nothing is emailed. The sign-in code is the only email a friendo site sends, so a
site needs no mail setup beyond what signing in already asks for. A note about
something you did yourself is never written, and the same thing done twice leaves
one note, so an inbox doesn't fill up with repeats.

The inbox has no switch of its own. It's on while follows or groups is, since
those are what write to it.

## The graph belongs to the site

Follows, groups and profiles are rows in one site's database. They aren't shared
across a network: following Pat on one site says nothing about Pat on another, and
a network account is only how you sign in to manage your sites. A site is a folder,
and its people stay with it, whether it runs on your laptop, your server or
friendo.world.

That's also how the graph moves. `friendo push --users` carries accounts, their
profiles and the follows between them to a deployed site, and `friendo pull
--users` brings them back. `friendo push --posts` carries posts, their events and
the group memberships, since a membership belongs to its group. Inbox notes don't
travel: they're about what happened on that site. See
[Push and pull](/docs/push-and-pull).

## Switching it off

**Follows** and **Groups** are two switches in **Settings → Features**, `follows`
and `groups` under `[settings]` in friendo.toml, and both start on. Off, the
feature's API refuses, its tags render nothing, and the template side reads empty:
`user.following` and `user.groups` are empty lists and `by_following` finds
nothing. What people already did is kept, so turning a switch back on brings it
all back.

**Next:** [Add profiles and follows](/docs/profiles), [Groups](/docs/groups), or
[Server-rendered and live](/docs/two-spellings) for how the same data shows up with
and without JavaScript.
