---
title: Community features
slug: community
group: Concepts
weight: 8
---

Friendo sites can host **comments, reactions, and polls** written by visitors —
with no custom JavaScript. You drop in a Web Component, a visitor signs in with
their email, and they can join the conversation. This page shows how to add and
style them; for the identity model underneath, see [Auth & users](/docs/auth).

## The `friendo.js` SDK

Every runtime serves the SDK at **`/friendo.js`**. Load it once (in your layout),
then use the components anywhere:

```html
<script src="/friendo.js" defer></script>

<friendo-auth></friendo-auth>
<friendo-reactions target-type="post" target-id="{{ record.id }}"></friendo-reactions>
<friendo-poll poll-id="…"></friendo-poll>
<friendo-comments post-id="{{ record.id }}"></friendo-comments>
```

Inside a post template, `{{ record.id }}` is the post's stable id — use it as the
`post-id` / `target-id`. The components call the site's own `/_/api` endpoints
themselves, so nothing else is needed.

| Component | Attributes | What it does |
|---|---|---|
| `<friendo-auth>` | — | Passwordless email sign-in (one-time code). Shows who's signed in and a sign-out button. |
| `<friendo-comments>` | `post-id` | Lists approved comments and, for signed-in members, a compose box. |
| `<friendo-reactions>` | `target-type`, `target-id`, `emojis` (optional, comma-separated; default `👍,❤️,🎉`) | Emoji reactions with live counts; click toggles yours. |
| `<friendo-poll>` | `poll-slug` (or `poll-id`) | Renders a poll; signed-in members vote once and see the tally. |
| `<friendo-channel>` | `channel-id` | A **realtime** message feed; signed-in members post and delete their own, and new messages stream in live. |

## Members: passwordless visitors

A visitor becomes a **member** by verifying their email with a one-time code —
no password. `<friendo-auth>` drives that flow: enter an email, receive a code,
enter it, done. In local development (no email provider configured) the code is
shown right in the form so you can test without sending mail; see
[Sending email](#sending-email) to wire up a real provider.

Members are authenticated enough to comment, react, and vote, but have no access
to the admin UI.

## Moderation

New comments start **pending** and are hidden from visitors until an admin
approves them. Manage them in the admin UI under **Comments**: each pending
comment can be **approved**, **rejected**, or **deleted**, and you can review the
approved and rejected sets from the same screen.

Prefer to skip review? Turn on **Auto-approve comments** in admin **Settings** and
new comments publish immediately.

**Moderate right on the page.** `<friendo-comments>` is author-aware: when the
signed-in viewer is the post's author (or a full moderator), it shows the pending
comments with inline **Approve / Reject / Delete** controls — no trip to the admin
UI. A signed-in member also sees their own pending comment (marked pending) and can
**delete** any comment they wrote.

Reactions and poll votes are never moderated — they're one-per-member and simply
toggle.

## Styling with `::part()`

Each component renders into a shadow root and ships almost no styling of its own.
Style it from your own stylesheet through the `part` attributes it exposes:

```css
friendo-comments::part(submit) { background: rebeccapurple; color: #fff; border-radius: 999px; }
friendo-comments::part(comment) { border: 1px solid #eee; border-radius: 10px; padding: .6rem .8rem; }
friendo-reactions::part(button)[aria-pressed="true"] { background: #eef; }
friendo-poll::part(bar) { background: #eef; }
```

Available parts:

| Component | Parts |
|---|---|
| `friendo-auth` | `form`, `email`, `code`, `button`, `status`, `signed-in`, `name`, `logout` |
| `friendo-comments` | `list`, `comment`, `author`, `badge`, `body`, `actions`, `approve`, `reject`, `delete`, `form`, `input`, `submit`, `status`, `empty`, `signed-out` |
| `friendo-reactions` | `row`, `button`, `emoji`, `count` |
| `friendo-poll` | `question`, `option`, `bar`, `result`, `total` |

Reaction and poll buttons carry `aria-pressed="true"` when they reflect the
member's own reaction / vote, so you can style the selected state.

## Events

The SDK dispatches DOM events you can hook into:

- **`friendo:auth`** — fired on `document` when a member signs in or out;
  `event.detail.user` is the member (or `null`). All components on the page
  refresh automatically.
- **`friendo:needs-auth`** — bubbles from `<friendo-reactions>` / `<friendo-poll>`
  when a signed-out visitor tries to act. Catch it to scroll to your
  `<friendo-auth>` element or open a sign-in prompt.

## Adding a poll

The simplest way to add a poll is to **declare it in a post's front matter** and
give it a `slug`. The runtime creates the poll the first time it's viewed — no
admin API call and no ids to copy:

```yaml
---
title: Cats or dogs?
poll:
  slug: cats-or-dogs
  question: Cats or dogs?
  options: [Cats, Dogs]
  # closes_at: 2026-12-31T00:00:00Z   # optional — stop accepting votes after this
---
```

Reference it from the post template by that slug:

```html
{% if record.data.poll %}
  <friendo-poll poll-slug="{{ record.data.poll.slug }}"></friendo-poll>
{% endif %}
```

That's it. The definition lives in the post's `data` (which syncs to deployed
sites via `friendo push`), so the same poll works locally and in production.

**Editing a poll.** Change the question or option labels in the front matter and
the running poll's text updates in place the next time it's viewed —
**existing votes are kept**. Removing or reordering options can make earlier
votes misleading, so prefer editing labels only; to start over, give the poll a
new `slug` (that's a brand-new poll).

**Creating a poll directly.** You can also create a slug-less poll through the
admin API and reference it by its returned `id` with `poll-id`:

```bash
curl -X POST https://yoursite.example/_/api/polls \
  -H 'Content-Type: application/json' --cookie 'friendo_session=…' \
  -d '{"post_id":"<post id>","question":"Cats or dogs?","options":["Cats","Dogs"]}'
```

```html
<friendo-poll poll-id="THE_RETURNED_ID"></friendo-poll>
```

## Uploading images

Attach an image to any record (a post, a page — anything with an id) through the
media API. Editors and contributors (anyone who can create content) may upload;
the file is stored in your site's `assets/` folder and served from `/assets/`,
exactly the same locally and on a deployed site.

```bash
curl -X POST https://yoursite.example/_/api/files \
  --cookie 'friendo_session=…' \
  -F record_type=post -F record_id=<post id> -F field=cover \
  -F file=@photo.jpg
```

The response includes a public `url` you can drop straight into a template or a
post body:

```json
{ "file": { "id": "…", "url": "/assets/uploads/….jpg", "mime": "image/jpeg", "size": 20481 } }
```

List a record's images with `GET /_/api/files?record_type=post&record_id=<id>`
(public), and remove one with `DELETE /_/api/files/<file id>`. Only images are
accepted, up to 10 MB.

## Tagging a location

Pin a latitude/longitude to any record — useful for event maps or "where this was
taken." Reads are public; attaching or removing a pin is an editor task.

```bash
curl -X POST https://yoursite.example/_/api/locations \
  --cookie 'friendo_session=…' -H 'Content-Type: application/json' \
  -d '{"target_type":"post","target_id":"<post id>","lat":40.7128,"lng":-74.006,"label":"NYC"}'
```

Fetch them with `GET /_/api/locations?target_type=post&target_id=<id>` (public)
and remove one with `DELETE /_/api/locations/<location id>`.

## Sending email

Until an email provider is configured, `<friendo-auth>` shows the sign-in code in
the form (handy for local dev) and the `request-code` API echoes it. To send real
email, set two environment variables on the runtime:

```
RESEND_API_KEY=…            # a Resend API key
FRIENDO_EMAIL_FROM=Your Site <hello@yoursite.example>
```

With both set, codes are emailed and never echoed. Requests are rate-limited to
one live code per address at a time.
