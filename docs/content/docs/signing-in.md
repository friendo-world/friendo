---
title: Signing in & roles
slug: signing-in
section: Concepts
weight: 7
---

Every friendo site works the same way: **people sign in with a code sent to their
email**, and everyone who does is a **member**. There is no password unless you
turn passwords on. Some members have a **role** that gives them more to do.

Think of a site as one big group. Its **admins** run it, its **moderators** keep it
tidy, and everyone else is a member. A [group](/docs/groups) inside the site uses
the very same words.

## On your own machine: nothing to sign in to

`friendo serve` on your laptop opens the admin (`/_/`) without a sign-in. You're
the owner. With no email provider set up there'd be nowhere to send a code, and a
site on `localhost` is yours.

The rule is strict: the admin only opens for a request from the same machine, to
`localhost`, with nothing in front of it. Put the site behind a proxy or on a real
hostname and sign-in is required. To see the sign-in screens locally anyway, run
`friendo serve --require-login`.

It's the *admin* that opens, not the site: on your pages, `<friendo-signin>` still
treats you as a visitor until you sign in with a code (which works locally, the
code shows on the page). So community features are testable locally as they'll
behave for real people.

## First run on a server

The first time anyone opens the admin of a site that's on the internet, they
create the site's **owner**: enter an email, get a 6-digit code, enter the code.
No account exists until the code checks out, so an unclaimed site can't be grabbed
by a bot that gets there first.

If the site has no email provider yet, the code is also printed in the terminal
running `friendo serve`, so you can finish setup from the server.

A site you publish with `friendo deploy` never shows this screen: the network
already made you its owner.

## Signing in

The admin's sign-in screen asks for your email and sends a code. That's it, for
every role.

**Allowing passwords.** In admin **Settings → Signing in**, turn on *Allow signing
in with a password*. The sign-in screen then also offers "Use a password instead",
and the member form gets an optional password field. Codes keep working for
everyone regardless.

For an owner or admin who signs in by code, their email account's security *is*
the site's security. On a production site, configure a real email provider
(`RESEND_API_KEY` + `FRIENDO_EMAIL_FROM`); without one, sign-in codes can only
reach the server log.

## Members and roles

Everyone with an account is a member. A member with no role can comment, react,
vote, RSVP, follow people and join groups. Roles add powers, and each one includes
the one below it:

| Role | Adds |
|---|---|
| **Owner** | Makes and removes admins and other owners; transfers the site |
| **Admin** | Runs the site: settings, people, deploys. An admin of every group |
| **Editor** | Writes and edits **any** post, and publishes |
| **Moderator** | Approves and rejects comments and posts waiting for review, without editing them. A moderator of every group |
| **Contributor** | Writes and edits **their own** posts, and reviews comments on them |

The line that matters most is between a contributor (their own posts) and an
editor (anyone's). A moderator sits between them: they can say yes or no to what
others wrote, but not change it.

A site always keeps at least one owner. To step down, make someone else an owner
first. Deleting a site altogether is an operator's job on the network, not a role.

Admins add people or change roles from **Members** in the admin. Adding someone
needs only their email; they sign in with a code.

## Pages that know who's looking

`{{ user.name }}` renders in a template, and `{% members only %}` at the top of
a page (or `moderators only`, `editors only`, …) keeps it for the people it's meant
for. Visitors see your `login.html` instead. See
[Members-only pages](/docs/templates#members-only-pages).

## Presets

Different sites want different defaults. In admin **Settings → Members & roles**,
pick a preset or set the pieces yourself:

| Preset | Anyone can sign up | New members start as contributors | Contributors' posts wait for review |
|---|---|---|---|
| **Personal** | off | off | off |
| **Community** | on | on | off |
| **Blog** | on | off | on |

The pieces, each also a key in [`friendo.toml`](/docs/config#settings):

- **Anyone can sign up** (`open_signups`). Off, only people an admin adds have accounts.
- **New members start as contributors** (`signups_are_contributors`). Off, a new
  member can comment, react, vote and RSVP but not post.
- **Contributors' posts wait for review** (`posts_need_review`). On, a contributor's
  post sits in the review queue until a moderator approves it.
- **Members can post** (`members_can_post`). On, any member can post from a
  [`<friendo-form>`](/docs/community#posting-from-a-page); their posts always wait for review.
- **Comments wait for review** (`comments_need_review`, on by default).
- **Members can start groups** (`members_can_start_groups`).

## Profiles

An account is who signs in; a **profile** is what other people see: a name, an
avatar, a short bio and a page at `/profiles/<slug>`. Every account gets one when
it's created, named after the account: Pat becomes `/profiles/pat` (a second Pat is
`pat-2`). An account may keep more than one profile (a real name and a pen name,
say) and picks which one its posts, comments and messages are by. Everything a
person writes points at the profile, not the account.

**The profile page.** Add `pages/profiles/[slug].html` and every profile has a
page. `profile` is the profile: `name`, `slug`, `avatar`, `bio`, `url`, `fields`,
plus `profile.posts` (their published posts), `profile.comments`,
`profile.followers` and `profile.following`. `collections.profiles` lists everyone.
`friendo init` scaffolds the page. On any post, `post.author` is the writer's profile.

```html
<h1>{{ profile.name }}</h1>
<p>{{ profile.bio }}</p>
<friendo-follow profile-id="{{ profile.id }}"></friendo-follow>
<friendo-profile slug="{{ profile.slug }}" edit-only></friendo-profile>
```

**Editing.** People edit their own profiles from the *profiles* switcher in
`<friendo-signin>`, or with [`<friendo-profile>`](/docs/components#friendo-profile)
on the profile page.

**More fields.** Declare them once and the editor, the tag and `profile.fields`
all know about them, with the same grammar as a collection's fields:

```toml
[profiles.fields]
pronouns = "text"
website  = { kind = "text", hint = "https://…" }
```

**Who sees profiles.** `profile_visibility` (admin **Settings → Members & roles**,
or `[settings]` in friendo.toml) is `members` by default: a visitor who opens a
profile page sees the sign-in page, exactly like a members-only page, and
`collections.profiles` is empty for them. Set it to `public` and everyone can see
them (the Community and Blog presets do). A profile never includes the email.

## Accounts travel

`friendo push --users` carries accounts and profiles to a deployed site, password
hashes included, so a password set locally works on the deployed site too (if it
allows passwords). `friendo pull --users` brings them back.

## Your site vs. your network account

If you host on a **network** like friendo.world, there are two different sign-ins:

- **Your site's members**, above: its owner, its staff, its community.
- **Your network account**: your account *on* the network, which owns your sites.
  Also an email code; `friendo login` runs the same flow from the terminal.

They stay separate, but the network bridges them for you: `friendo deploy` signs
you into your site's admin from your network sign-in, and the **Open admin** button
on [your account page](/docs/network-account) does the same in the browser. You
sign in once, not twice.
