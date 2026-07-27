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
<friendo-map target-type="post" target-id="{{ record.id }}"></friendo-map>
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
| `<friendo-map>` | `target-type`, `target-id` | An interactive [Leaflet](https://leafletjs.com/) map of a record's [locations](#tagging-a-location), one marker per pin. Public — no sign-in needed. |
| `<friendo-form>` | `collection`, `redirect` (optional), `status` (optional) | A create-a-post form: the author's own inputs (plus rich `<friendo-input>` types) become a new post, submitted from the page. See [Submitting posts from a page](#submitting-posts-from-a-page). |

`<friendo-map>` loads Leaflet (open-source) and OpenStreetMap tiles from a CDN the
first time a map appears on a page — no API key, and the rest of the SDK stays
dependency-free.

## Rendering without JavaScript

The components above are interactive and load client-side. For content that should
be readable **without JavaScript** — approved comments, reaction tallies, a poll's
results — both runtimes also attach the public data straight to the post's `record`,
so a template can render it inline:

```html
<h2>Reactions</h2>
<ul>{% for r in record.reactions %}<li>{{ r.emoji }} {{ r.count }}</li>{% endfor %}</ul>

{% if record.poll %}
  <h3>{{ record.poll.question }}</h3>
  <ul>{% for o in record.poll.options %}<li>{{ o.text }} — {{ o.votes }}</li>{% endfor %}</ul>
{% endif %}

<h2>Comments</h2>
<ol>{% for c in record.comments %}<li><b>{{ c.author_name }}</b>: {{ c.body }}</li>{% endfor %}</ol>
```

`record.comments` is the **approved** comments only; `record.poll` is present when
the post declares one in front matter. These render the same bytes on both runtimes.
The template form and the `<friendo-*>` component are two spellings of the same data
— use whichever a page needs (often the server-rendered list for readers/SEO *and* a
component for signing in and posting). See [Templates](/docs/templates#community-relations)
for the full list of relations. Realtime chat and the map are interactive by nature,
so they have no server-rendered form.

## Personas

An account can keep more than one **persona** (author profile) — say a real name and
a pen name — and choose which one their comments and messages are attributed to.
`<friendo-auth>`, when signed in, shows the current persona with a **personas**
switcher: pick a different one, or add a new one inline.

Under the hood a persona is an `authors` row; the account's chosen default is a
pointer (`users.default_author_id`) that all attribution honors, so switching persona
re-labels new posts everywhere at once. The API is member-facing:

```
GET  /_/api/me/personas                 # your personas (is_default flags the current one)
POST /_/api/me/personas                 # { "name": "Pen Name" } → create one
POST /_/api/me/personas/<id>/default    # make it your default
```

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
| `friendo-auth` | `form`, `email`, `code`, `button`, `status`, `signed-in`, `name`, `logout`, `personas-toggle`, `personas`, `persona`, `new-persona`, `new-name`, `add` |
| `friendo-comments` | `list`, `comment`, `author`, `badge`, `body`, `actions`, `approve`, `reject`, `delete`, `form`, `input`, `submit`, `status`, `empty`, `signed-out` |
| `friendo-reactions` | `row`, `button`, `emoji`, `count` |
| `friendo-poll` | `question`, `option`, `bar`, `result`, `total` |
| `friendo-map` | `map`, `empty` |
| `friendo-input` | `input`, `toolbar`, `tool`, `editor`, `map`, `coords`, `file`, `preview`, `note`, `chips`, `chip`, `chip-remove` |
| `friendo-form` | `status`, `error` |

Reaction and poll buttons carry `aria-pressed="true"` when they reflect the
member's own reaction / vote, so you can style the selected state.

`<friendo-form>` is the one exception to the shadow-root rule: it's a light-DOM
controller (so your own inputs stay yours — your CSS applies and your submit button
drives it), which means its `status`/`error` parts aren't reachable with `::part()`.
Style them by attribute instead — `friendo-form [part="error"] { … }`. Each
`<friendo-input>` *is* a shadow component, so its parts style with `::part()` as usual.

## Events

The SDK dispatches DOM events you can hook into:

- **`friendo:auth`** — fired on `document` when a member signs in or out;
  `event.detail.user` is the member (or `null`). All components on the page
  refresh automatically.
- **`friendo:needs-auth`** — bubbles from `<friendo-reactions>` / `<friendo-poll>`
  when a signed-out visitor tries to act. Catch it to scroll to your
  `<friendo-auth>` element or open a sign-in prompt.
- **`friendo:submitted`** — bubbles from `<friendo-form>` after it creates a post;
  `event.detail.record` is the new record. Use it to update the page or show a
  confirmation (an alternative to the `redirect` attribute).

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

Uploaded images travel on deploy: `friendo push --data` carries both the image
bytes (in `assets/uploads/`) and the record link, so a deployed site shows the
same media as your local one.

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

To **show** the pins, drop in `<friendo-map>` — it reads the same endpoint and
renders an interactive map:

```html
<friendo-map target-type="post" target-id="{{ record.id }}"></friendo-map>
```

## Submitting posts from a page

Comments, reactions, and polls let visitors add *to* a post. `<friendo-form>` lets
them create *a whole post* — a title, a body, and any extra metadata — straight from
a page on your site, with no admin UI, no endpoint knowledge, and no schema edit. You
write an ordinary form; the component turns it into a created post.

```html
<script src="/friendo.js" defer></script>

<friendo-form collection="blog" redirect="/blog/{slug}">
  <input name="title" placeholder="Title" required />
  <friendo-input name="body" type="richtext" placeholder="Write your story…"></friendo-input>
  <friendo-input name="where" type="location"></friendo-input>
  <friendo-input name="cover" type="media" accept="image/*"></friendo-input>
  <friendo-input name="tags" type="tags"></friendo-input>
  <select name="mood"><option>calm</option><option>hyped</option></select>
  <button type="submit">Publish</button>
</friendo-form>
```

### The one rule: field names decide where values land

**Reserved names become post columns; every other name becomes metadata under
`data`.** That's the whole convention — guessable without docs.

| Field name | Lands as |
|---|---|
| `title` | the post's title |
| `body` | the post's body |
| `slug` | the post's slug (auto-derived from `title` if you omit it) |
| `status` | requested status (clamped server-side — see below) |
| *anything else* (`mood`, `tags`, `where`, `cover`…) | `data.<name>` |

So the form above creates a post whose `data` is
`{ "where": {lat,lng}, "cover": "<url>", "tags": [...], "mood": "calm" }` — and those
read straight back in a template, no extra wiring:

```html
<p>Mood: {{ record.data.mood }}</p>
<ul>{% for t in record.data.tags %}<li>{{ t }}</li>{% endfor %}</ul>
```

### Rich inputs: `<friendo-input>`

Native `<input>`, `<textarea>`, and `<select>` work as-is. For richer values, drop in
a `<friendo-input>` with a `type` — `<friendo-form>` reads its value at submit time:

| `type` | Renders | Value shape |
|---|---|---|
| `richtext` | a **WYSIWYG** editor ([TipTap](https://tiptap.dev/)) — bold, italic, H2/H3 headings, bullet & numbered lists, blockquote, inline code, and links, all shown live as you type | a **markdown** string (so it still flows through the `markdown` filter and fits the `body` column) |
| `location` | a click-to-pick [Leaflet](https://leafletjs.com/) map | `{ lat, lng }` in `data.<name>` |
| `media` | a file/image picker with preview | uploaded after the post is created; its public URL is stored in `data.<name>` |
| `tags` | a chip input | a `string[]` |

The rich inputs lean on machinery loaded only when they appear on a page, so the rest
of the SDK stays lean: `richtext` lazy-loads [TipTap](https://tiptap.dev/) and
`location`/`media` use [Leaflet](#tagging-a-location) and the
[media API](#uploading-images). Both editors load their library from a CDN the first
time they're used; if `richtext` can't reach it, it falls back to a plain Markdown
textarea, so the form always works.

**A location field becomes a map pin automatically.** When a post is created with a
`{ lat, lng }` value in its `data` (which is exactly what the `location` input
produces), the runtime geo-tags the post server-side — so
`<friendo-map target-type="post" target-id="{{ record.id }}">` shows the pin with no
extra call. Because it happens on the server, it works for member submissions too,
which can't reach the contributor-gated locations API directly.

### After submit: redirect or handle it yourself

Give `<friendo-form>` a `redirect` and it navigates there on success, substituting
`{slug}` and `{id}` from the new record (`redirect="/blog/{slug}"`). Omit it and the
form resets and shows a confirmation in its `status` part instead. Either way it fires
a **`friendo:submitted`** event whose `event.detail.record` is the created post, so you
can update the page, show a toast, or drive a custom redirect.

### Who can submit

By default only **contributors and above** (anyone who can create content) may post —
their submission publishes or waits per your normal [approval setting](/docs/auth), no
different from authoring in the admin UI.

Ordinary **members** (passwordless community accounts) are blocked unless you opt in:
turn on **Members can submit posts** in admin **Settings** (the
`content.accept_submissions` setting). When it's on, a signed-in member may submit, and
their post is **forced into the review queue as `pending`** — a requested `published`
status is ignored — so an editor approves it exactly like a pending comment before it
goes live. Member submissions are rate-limited to keep the opened surface from being
spammed. When the setting is off, members get a `403`.

> **Media in member submissions.** In this first cut the `media` input is a
> contributor+ affordance — a member sees a note instead of a picker, so their
> submission simply carries no file. Contributors and above upload normally.

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
