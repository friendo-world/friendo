---
title: Troubleshooting
slug: troubleshooting
section: Guides
topic: Hosting
weight: 70
description: Common problems with friendo, what causes each one, and how to fix it.
---

Find the symptom, check the cause, apply the fix. Each section links to the page
with the full story.

## command not found

**Cause.** Your shell can't find the `friendo` binary. The installer puts it in
`/usr/local/bin`, or in `~/.local/bin` when `/usr/local/bin` isn't writable, and
that folder may not be on your `PATH`. `go install` puts it in `$(go env GOPATH)/bin`.

**Fix.** The installer prints `Note: … is not on your PATH` with the line to add.
Add the folder to your `PATH` in your shell's startup file, open a new terminal, and
check:

```bash
export PATH="$HOME/.local/bin:$PATH"
friendo --version
```

See [Installation](/docs/installation).

## The sign-in code never arrives

**Cause.** No email provider is set up. friendo sends codes through Resend, and only
when **both** `RESEND_API_KEY` and `FRIENDO_EMAIL_FROM` are set. With one of them
missing, nothing is sent.

**Fix.** Set both where the site runs, then restart it:

```bash
RESEND_API_KEY=re_… FRIENDO_EMAIL_FROM='My Site <hello@example.com>' friendo serve
```

Other things to check:

- **On your laptop**, with no provider, `friendo serve` shows the code on the page,
  in the sign-in form. No email is expected.
- **Setting up the owner** on a server with no provider: the code is printed in the
  terminal running `friendo serve`.
- **A new email on a site with sign-ups off** (`open_signups = false`) gets
  `sign-ups are disabled for this site`. An admin has to add the person first.
- **Asked twice in a row**: one code at a time. Wait, then ask again. A code lasts
  10 minutes.
- **Resend refused the message**: friendo doesn't report it. Check the email log
  in your Resend account.

See [Sending email](/docs/email) and [Environment variables](/docs/env#email).

## The admin asks me to sign in on localhost

**Cause.** `friendo serve` opens the admin without a sign-in only when all of these
hold:

- The request comes from the same machine, to `localhost`, `127.0.0.1` or `::1`.
  A LAN address, a `.local` name, a tunnel or a proxy in front all count as not local.
- No email provider is set (`RESEND_API_KEY` and `FRIENDO_EMAIL_FROM` aren't both set).
- You didn't pass `--require-login` or set `FRIENDO_REQUIRE_LOGIN`.
- It's `friendo serve`. `friendo network serve` always asks.

Also: if you signed in with a code through `<friendo-signin>` on the site, the admin
sees that account, not the open-mode owner.

**Fix.** Open `http://localhost:3000/_/`, unset the email variables in that
terminal, and restart `friendo serve`. Or sign out on the site. Or sign in with the
code: locally it shows on the page. See
[Signing in & roles](/docs/signing-in#on-your-own-machine-nothing-to-sign-in-to).

## Port 3000 is already in use

**Cause.** Another program, often a second `friendo serve`, is listening on port
3000. friendo stops with `listen tcp :3000: bind: address already in use`.

**Fix.** Stop the other program, or pick another port:

```bash
friendo serve --port 3001
```

On macOS and Linux, `lsof -i :3000` shows what holds the port. See the
[CLI reference](/docs/cli).

## deploy says the subdomain is taken or reserved

**Cause.** `friendo deploy` with no name uses your site's name as the subdomain.

- `that subdomain is taken`: another account owns a site with that name. Deploying
  again to a site you own is fine.
- `"www" is reserved on this network`: the network keeps some names for itself:
  `www`, `api`, `admin`, `mail`, `origin`, `network`, `console`, `account`,
  `login`, `activate`, `docs`, `static`, `cdn`. Only an operator can create them.

**Fix.** Name another subdomain:

```bash
friendo deploy my-club-2
```

See [Deploy](/docs/deploy).

## A members-only page is missing from my static export

**Cause.** This is on purpose. A static host serves every file to everyone and can't
tell who is signed in, so `friendo export --mode static` skips any page with a
`{% members only %}`-style tag or a path in `[access]`, and prints
`Skipped <page> (members only)`. A collection whose page is members-only is also
left out of `calendar.ics`. Profile pages are exported only when
`profile_visibility` is `public`, and a group that isn't public is left out with
its posts.

**Fix.** Keep the page members-only and serve the site with friendo
([Self-host a site](/docs/self-host-a-site) or [Deploy](/docs/deploy)), or remove
the gate if the page is meant to be public. See
[Static export](/docs/static-export) and [Members-only pages](/docs/members-only).

## The rich text editor is a plain textarea

**Cause.** `<friendo-input type="richtext">` loads its editor (TipTap) from
`esm.sh`, a CDN, the first time it's used. If the browser can't reach it (offline,
a blocker extension, a network that filters it), the tag falls back to a textarea
with the note *Rich editor unavailable — write Markdown here.*

**Fix.** Allow `esm.sh` and reload. The textarea still works: write Markdown, and
the post's body renders the same. See [Forms](/docs/forms).

## A post has no title or fields

**Cause.** The front matter isn't valid YAML, so friendo drops all of it without a
warning. The post gets its file name as the title, `published` as the status and no
fields. The usual culprit is a value with `: ` (a colon and a space) in it.

**Fix.** Quote the value:

```diff
-title: Rome: a week on foot
+title: "Rome: a week on foot"
```

Also check the template: a field is `post.fields.mood`, not `post.mood` or
`post.data.mood`. See [Post front matter](/docs/front-matter#quoting).

## Changes to content/ don't show

**Cause.** One of these:

- **A folder made while `friendo serve` was running.** It watches the folders that
  existed when it started. Edits in a folder added later (a new collection, a new
  post-as-a-folder) aren't picked up.
- **The file has a problem.** The terminal prints `Content: imported … ; 1 warning(s)`
  but not the warning itself.
- **The post isn't published.** `status: draft` or `pending` keeps it off the site.
- **It's the deployed site.** A plain `friendo push` sends templates and assets,
  not posts.

**Fix.** Restart `friendo serve`, or run `friendo import` to read `content/` now
and print each warning:

```bash
friendo import
```

For a deployed site, push the posts:

```bash
friendo push --posts
```

See [Write posts in files](/docs/posts-in-files) and
[Push and pull](/docs/push-and-pull).

## Google Calendar doesn't show my events

**Cause.** One of these:

- **The site isn't online.** Google fetches the feed from its own servers, so a
  `localhost` feed can't work.
- **Google hasn't refreshed yet.** It checks subscribed feeds slowly, often once
  or twice a day.
- **The link downloads a file.** A link straight to `/calendar.ics` downloads it;
  Google wants a subscription link.
- **The events aren't in the public feed.** Only published posts with a `when` go
  out. A collection whose page is members-only, and a private group's events, are
  left out.

**Fix.** Deploy the site, then subscribe with the Google link from the `calendar`
variable, or with `<friendo-add-to-calendar subscribe>`:

```html
<a href="{{ calendar.google }}">Google Calendar</a>
```

Members see members-only events through their private calendar link. See
[Calendar](/docs/calendar#subscribe-calendarics).

**Next:** [Upgrade friendo](/docs/upgrade) · [Environment variables](/docs/env)
