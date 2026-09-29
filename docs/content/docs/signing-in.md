---
title: Members, roles and sign-in
slug: signing-in
section: Concepts
weight: 50
description: People sign in with a code sent to their email, everyone who does is a member, and roles add powers.
---

Every friendo site works the same way: **people sign in with a code sent to their
email**, and everyone who does is a **member**. There is no password unless you
turn passwords on. Some members have a **role** that gives them more to do.

Think of a site as one big group. Its **admins** run it, its **moderators** keep it
tidy, and everyone else is a member. A [group](/docs/social-graph) inside the site
uses the very same words.

## Why a code and not a password

A password is one more thing for a member to make up, forget and reuse. An
emailed code proves the one thing friendo needs to know, that this person can
read that inbox, and it works for the owner setting up a site and for a visitor
who wants to leave one comment. The cost is that an admin's email account *is*
the site's security, so a production site should use a real
[email provider](/docs/email) rather than codes in a server log.

Passwords are there for the people who want them. In admin **Settings → Signing
in**, turn on *Allow signing in with a password*; the sign-in screen then also
offers "Use a password instead", and codes keep working for everyone regardless.

## On your own machine: nothing to sign in to

`friendo serve` on your laptop opens the admin (`/_/`) without a sign-in. You're
the owner. With no email provider set up there'd be nowhere to send a code, and a
site on `localhost` is yours.

The rule is strict: the admin only opens for a request from the same machine, to
`localhost`, with nothing in front of it. Put the site behind a proxy or on a real
hostname and sign-in is required. To see the sign-in screens locally anyway, run
`friendo serve --require-login`.

It's the *admin* that opens, not the site: on your pages, `<friendo-signin>` still
treats you as a visitor until you sign in with a code (which works locally; the
code shows on the page). So community features are testable locally exactly as
they'll behave for real people.

## First run on a server

The first time anyone opens the admin of a site that's on the internet, they
create the site's **owner**: enter an email, get a 6-digit code, enter the code.
No account exists until the code checks out, so an unclaimed site can't be grabbed
by a bot that gets there first.

If the site has no email provider yet, the code is also printed in the terminal
running `friendo serve`, so you can finish setup from the server.

A site you publish with `friendo deploy` never shows this screen: the network
already made you its owner.

## Members and roles

A visitor becomes a member by entering their email in `<friendo-signin>` and
typing the code it sends. A member with no role can comment, react, vote, RSVP,
follow people and join groups. Roles add powers, and each one includes the one
below it: a **contributor** writes and edits their own posts; a **moderator** says
yes or no to what others wrote, without changing it; an **editor** writes and edits
anyone's posts and publishes; an **admin** runs the site; an **owner** makes and
removes admins and other owners.

The line that matters most is between a contributor (their own posts) and an
editor (anyone's). A moderator sits between them. The whole matrix, and the
presets that set a site up as a personal site, a community or a blog, is in
[Roles and permissions](/docs/roles).

A site always keeps at least one owner. To step down, make someone else an owner
first. Deleting a site altogether is an operator's job on the network, not a role.

## Accounts and profiles

An **account** is who signs in: an email address. A **profile** is what other
people see: a name, an avatar, a bio, a page at `/profiles/<slug>`. Every account
gets one when it's created, and may keep more than one (a real name and a pen
name, say). Everything a person writes points at a profile, never at the
account, and a profile never includes the email. See
[The social graph](/docs/social-graph) for how profiles, follows and groups fit
together, and [Add profiles and follows](/docs/profiles) to put them on a page.

## Your site and the network

If you host on a **network** like friendo.world there are two different sign-ins:
your site's members, above, and your **network account**, which owns your sites.
They stay separate, but the network bridges them so you sign in once, not twice.
See [Your site and the network](/docs/site-vs-network).
