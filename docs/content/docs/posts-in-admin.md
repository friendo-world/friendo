---
title: Write posts in the admin
slug: posts-in-admin
section: Guides
topic: Content
weight: 20
description: Every site has an admin at /_/ with a table and a form for every collection.
---

Every site has an admin at `/_/`, the same one on your laptop and on the deployed
site. It's the place to write when you don't want to touch files, and the place
to see what members have written.

## Open it

Run `friendo serve` and open **http://localhost:3000/_/**. On your own machine
there's nothing to sign in to: the admin opens and you're the owner. On a
deployed site you sign in with a code sent to your email; see
[Members, roles and sign-in](/docs/signing-in).

## Write a post

Pick a collection in the sidebar. Its posts show as a table whose columns are
the fields they carry. **New post** opens the form: title, slug (filled in from
the title), body in markdown, status, and every field the collection
[declares](/docs/config#fields), plus **When** and **Location** sections that
make the post an event or put it on a map.

A post is a **draft** until you publish it. Only published posts appear on the
site, in `collections.*`, and in the calendar feed.

## Add a field

*Add a field* on any post puts a new key on it, with no schema to change. Read it
back in a template as `{{ post.fields.<name> }}`. To give every post in the
collection the same field, with a kind and a hint in the form, declare it in
[friendo.toml](/docs/config#fields).

## Whose post is it

A post has an author, the profile of whoever wrote it. An **editor** can write and
edit any post; a **contributor** only their own. What members can do is in
[Roles and permissions](/docs/roles).

> **Note:** a post that came from a `content/` file says so in the admin. The file
> wins on the next import, so edit those in the file. See
> [Write posts in files](/docs/posts-in-files).

**Next:** [Let members post from a page](/docs/forms), or the
[tour of the admin](/docs/admin) for everything else in there.
