---
title: Send real email
slug: email
section: Guides
topic: Community
weight: 60
description: Set two environment variables so sign-in codes reach people's inboxes, and know what happens to codes until you do.
---

To send sign-in codes by email, set `RESEND_API_KEY` and `FRIENDO_EMAIL_FROM` on
the server. Friendo signs everyone in with a 6-digit code, so on a public site an
email provider isn't optional: it's how members, admins and the owner get in.

## 1. Get a Resend key

Friendo sends mail through [Resend](https://resend.com). Make an account, verify
the domain you'll send from, and create an API key.

## 2. Set the variables

Set both on the server, before it starts:

```bash
RESEND_API_KEY=re_…
FRIENDO_EMAIL_FROM='Your Site <hello@yoursite.example>'
```

For a [self-hosted site](/docs/self-host-a-site):

```bash
RESEND_API_KEY=… FRIENDO_EMAIL_FROM='My Site <hello@example.com>' friendo serve --port 3000
```

A [network](/docs/run-a-network) takes the same two variables, and every site on
it uses them.

## 3. Try it

Sign in with `<friendo-signin>` on one of your pages. The form says *Check
you@example.com*, and an email titled *Your sign-in code* arrives. The code works
for 10 minutes. Asking again within 30 seconds is refused; after that, a new code
replaces the old one.

With both variables set, codes are emailed and never shown on the page, whatever
else is set.

> **Warning:** set both. With `RESEND_API_KEY` but no `FRIENDO_EMAIL_FROM`,
> nothing is sent and the code isn't shown either, so nobody can sign in.

## Without an email provider

What happens to a code depends on `FRIENDO_OTP_ECHO`:

| | Codes |
|---|---|
| `friendo serve` with no `RESEND_API_KEY` and no `FRIENDO_OTP_ECHO` | Echo turns on by itself: the code comes back to the browser and `<friendo-signin>` shows it as *Dev code*, filled in. The server logs a warning. |
| `FRIENDO_OTP_ECHO=1` (or `true`, `yes`, `on`), no provider | The same, on purpose. Also works for `friendo network serve`, which never turns it on by itself. |
| `FRIENDO_OTP_ECHO=0`, no provider | Codes go nowhere. |
| A provider configured | Emailed. Echo is off no matter what. |

One code is always printed in the terminal: while a site has no owner yet, the
**setup code** for the first owner is logged when no email provider is set, so
you can finish [first-run setup](/docs/signing-in#first-run-on-a-server) from the
server. Member sign-in codes are never logged.

On your own machine none of this gets in the way: the admin opens without a
sign-in for requests from `localhost` when no email provider is set, and
`<friendo-signin>` shows the code so you can test as a member.

> **Warning:** never put a site on the internet with echo on. Echo hands the
> code to whoever asked for it, so anyone could sign in as anyone. On a public
> `friendo serve` with no email provider, that's the default: set the two
> email variables, or at least `FRIENDO_OTP_ECHO=0`.

## What gets emailed

Only sign-in codes. Everything else a member should know (a new follower, a
comment on their post, a group invite) goes to their inbox on the site, the
`<friendo-inbox>` tag. See [Profiles](/docs/profiles).

**Next:** [Self-host a site](/docs/self-host-a-site), or
[Environment variables](/docs/env) for every variable the server reads.
