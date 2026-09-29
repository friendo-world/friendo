---
title: A tour of the admin
slug: admin
section: Concepts
weight: 70
description: What each page of the admin at /_/ is for, who sees it, and how it relates to your friendo.toml.
---

Every friendo site has an **admin** at `/_/`. It's the same admin on your laptop,
on your own server and on friendo.world, and it's built into the runtime: there's
nothing to install. It covers what's awkward to do in files, like reviewing what
members wrote, managing people and flipping switches. The files stay the source of
truth for the site's shape.

## Opening it

On your own machine, `friendo serve` opens the admin without a sign-in, and you're
the owner. The rule is strict: the request has to come from the same machine, to
`localhost`, with nothing in front of it. On a real hostname, or with
`friendo serve --require-login`, the admin asks for your email and sends a code.
See [Signing in & roles](/docs/signing-in).

Only the admin opens this way. The site's pages still treat you as a visitor until
you sign in on them, and the owner the admin opens as has no profile, so following,
commenting or answering an RSVP takes a real sign-in on the page. That keeps
community features behaving locally the way they will for real people.

Across the top are the sections a role can use: **content** for contributors and
up, **review** for contributors and up, **members** and **settings** for admins and
owners, and a link back to the site.

## Content

The content area has your [collections](/docs/posts) down the left, each with how
many posts it holds. Collections that friendo.toml lists come first, in its order.
One that exists only because it has posts is marked ✱ until you add it to
`[content]`. **New collection** starts a post in a collection of that name; the
collection appears once the post is saved.

Pick a collection and its posts show as a table. The columns are the fields the
posts carry, plus status, slug, when, created and updated, and you choose which to
show; the choice is remembered in your browser for each collection. There's a
search box, every column sorts, the table shows fifty posts a page, and ticking
posts gives a bar to publish, unpublish or delete them together.

Opening a post slides the **post form** over the table, so the list keeps its
place. The form has the title and slug (the slug follows the title until you change
it), the body in markdown with an optional rich-text mode, and a **Fields** section
with every field the post carries. Fields the collection
[declares](/docs/config) come first, with their kind, choices and hint, and are
checked before saving; **Add a field** puts a new one on this post alone. Below
that, **When** gives the post a time and makes it an [event](/docs/calendar):
start, end, all day, how it repeats and until when, a custom rule, dates to skip,
and its timezone. **Location** puts it on the [map](/docs/locations) with a
latitude, a longitude and a place name; it's hidden while the Locations switch is
off.

An editor sets the status (draft, pending or published). A contributor sees the
status and a note that an editor publishes it. A post that came from a `content/`
file says so, since the next import writes the file over any change made here.

## An event's RSVPs

A post with a time has an **Attendees** button (while the RSVPs switch is on). It
opens the event's RSVPs page: who answered, grouped by date for a repeating event,
with a count of going, maybe, can't go and invited for each, a mark on dates that
are no longer scheduled, and a CSV download. Its **Invite people** box asks people
to the event by profile name, by a group's members, or everyone who follows you.
Each gets an RSVP waiting for their answer and a note in their inbox. See
[RSVPs](/docs/calendar).

## A group's members

A post in the `groups` collection has a **Members** section in its form, while the
Groups switch is on. It lists who's in, admins and moderators first, and who asked
to join, with buttons to approve or decline a request, make someone an admin or a
moderator, demote them, or remove them. **Add by profile address** puts someone in
by their profile slug. It's how a group written as a file gets its first admin,
since a file has no author. See [Groups](/docs/groups).

## Review

The **Review** page is everything waiting on a person. **Posts to review** lists
the posts waiting for review, for moderators and up; each can be opened, published or deleted. **Comments** lists
comments by status (pending, approved, rejected), each with approve, reject and
delete. Moderators and up see every comment; a contributor sees the ones on their
own posts. Requests to join a group aren't here; they're in the group's Members
section and in `<friendo-group>`. See [Review](/docs/review).

## Members

**Members** is a table of everyone with an account: name, a link to their profile
page, email and role. Adding someone takes an email, a name and a role; they sign in
with a code. When password sign-in is allowed, the form also takes an optional
password. Admins can give roles up to editor; only an owner can make admins and
other owners. Nobody can delete an owner or themselves from here. The roles are in
[Roles](/docs/roles).

## Settings

**Settings** is for admins and owners. Every setting on it has one name, the same
in the admin, the API and friendo.toml. Any key you set under
[`[settings]` in friendo.toml](/docs/config) wins: it's applied when the site
starts, and its control here turns read-only, with a note to edit the file. A key
you leave out stays editable here and travels to a deployed site with
`friendo push`.

**Site** shows the name, the number of collections and the number of members; the
name and collections come from friendo.toml. **Content** shows the `[content]`
block that matches the site right now, every collection and the fields its posts
carry, ready to copy into your file (`friendo pull` writes it for you). While
friendo.toml lists no collections, it also picks which built-in ones (`blog`,
`pages`) the sidebar shows, the `default_collections` key.

**Features** is the eight switches: Comments, Reactions, Polls, RSVPs, Locations,
Chats, Follows and Groups, the keys `comments`, `reactions`, `polls`, `rsvp`,
`locations`, `chats`, `follows` and `groups`. A switch turned off makes the site
refuse that feature everywhere at once, and keeps what people already wrote.

**Members & roles** starts with three presets, Personal, Community and Blog, each a
bundle of the settings below it. Then the settings themselves: *Anyone can sign up*
(`open_signups`), *New members start as contributors* (`signups_are_contributors`),
*Contributors' posts wait for review* (`posts_need_review`), *Members can post*
(`members_can_post`), *Members can start groups* (`members_can_start_groups`, shown
while Groups is on) and *Profiles are visible to* (`profile_visibility`). Setting
any of the three preset keys in friendo.toml locks the presets too.

**Signing in** has *Allow signing in with a password* (`password_login`); codes
keep working for everyone either way. **Comments** has *Comments wait for review*
(`comments_need_review`). **Your account** shows who you're signed in as, and a
sign-out button.

**Next:** [Write posts in the admin](/docs/posts-in-admin), or
[friendo.toml](/docs/config) for every setting as a key.
