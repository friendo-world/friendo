# Built-in calendar — events, feeds, and RSVP (plan)

> **Status: built (2026-09-25), all three phases.** Scoped 2026-09-24 from a read of the
> whole runtime; the decisions below were put to the user and settled. Landed as described:
> `runtime/go/data/{events,when,rsvps}.go`, `runtime/go/calendar`, the renderer's calendar
> filters, migrations `0013`–`0014`, `<friendo-calendar>` / `<friendo-rsvp>` /
> `<friendo-input type="when">` in the SDK, and the admin's When / Where / Attendees screens.
> Coverage: `when_test.go`, `rsvps_test.go`, `calendar/calendar_test.go`,
> `tests/calendar_test.go`, the REST scenarios, the export test, and the browser checks
> `sdk-calendar.mjs`, `admin-when.mjs`, `sdk-rsvp.mjs`. Departures from the plan are
> noted inline as *Built:* remarks. Builds on the `location` → `locations` pattern
> (`content.go` / `api.go` / `<friendo-map>`) and the `<friendo-form>` plan in
> [v0.3-friendo-form-plan.md](v0.3-friendo-form-plan.md).
>
> **Scope cut (same day):** a "slots" concept — bookable time windows with capacity
> and waitlists, for resource booking and volunteer shifts — was designed, found to
> carry most of the feature's complexity, and **deferred to a later release**. The
> sketch is kept at the end so that work starts from what was learned.

**Scope theme: an event is a post with a `when`.** A pin is already a post with a
`location`: the importer and the record API lift that one field out of opaque `data`
into a real, indexed row, and the map reads it back. Time gets the same treatment.
Everything else a calendar needs already exists — collections and `[slug].html` pages,
`<friendo-form>` for submissions, the pending review queue, member sign-in, comments
and reactions on the event page, `friendo push`, static export. The feature is one new
sidecar table, one lifting rule, a handful of filters, two feed URLs, and (phase 3) one
small table for RSVPs.

## What it must cover

| Use case | How it falls out |
|---|---|
| **Community events board** — people submit events, they appear on a calendar | `<friendo-form collection="events">` with a `when` input → pending review → published → listed by `collections.events\|upcoming` |
| **Subscribe in Google Calendar / Apple Calendar / Outlook** | `/calendar.ics` — every published post with a `when`, recurrence included, kept in sync as posts change |
| **Event pages** | `pages/events/[slug].html` — `record.when`, the pin, comments, reactions, an RSVP |
| **A weekly meet-up, a monthly book club** | `repeats: weekly` / `repeats: {every: month, on: first tuesday}` — **phase 1**, expanded server-side and emitted natively in the feed |
| **"Are you coming?"** | `<friendo-rsvp>` — going / not going / maybe, one answer per member per occurrence, counts on the page |

Not in this release: resource booking, volunteer shifts, capacity, waitlists. See
*Deferred: slots* at the end.

## The decisions

| # | Decision | Taken | Why |
|---|---|---|---|
| 1 | Field name | **`when`** (plus `ends`, `repeats`, `except`, `timezone`) | `date` is already the published date in front matter. `when` is the word a person uses. |
| 2 | Where time lives | **Sidecar `events` table**, one row per *series*, keyed by target like `locations` | Mirrors the one pattern the runtime already teaches; leaves `posts` untouched; indexed on `starts` so "upcoming" is a range scan, not a JSON crawl (the poll lookup's `findPollDef` scan is the anti-pattern); and keeps the door open to many rows per post when booking slots return, with no migration of `posts`. |
| 3 | Recurrence | **Phase 1**, not later | User's call. Most community calendars are *mostly* recurring; a board that can't say "every Tuesday" is not a calendar. |
| 4 | Feed URL | **`/calendar.ics` at the site root**, page-overridable | It's what a novice pastes into Google Calendar's "From URL"; under `/_/` it reads as admin. If a site's `pages/` defines that path, the page wins — the same rule as the apex default pages. |
| 5 | RSVP | **Own `rsvps` table**, three answers, **per occurrence** | Not polls: an RSVP is a per-person answer with a time attached, and on a weekly event "are you coming?" means *this* Monday. No capacity, no waitlist — those belong to slots, later. |
| 6 | Visibility | Feed is **published-only and public**; it **skips collections whose page is gated** | Per-record visibility doesn't exist yet; a members-only `events/` folder must not leak through the feed. Personal token feeds are the later answer. |
| 7 | Occurrences | **Expanded on read, never materialized** | Rows stay small and edits stay one-place; the feed hands the rule to the subscriber's app, which expands it itself. |
| 8 | Slots | **Deferred** | Bookable windows with capacity carried most of the complexity (per-occurrence keying, re-keying on edit, orphaned sign-ups, feed noise). Ship the calendar first. |

## What the author writes

### An event, in a content file

```yaml
---
title: Harvest Fair
when: 2026-10-04 10:00 to 16:00
location: 47.6062, -122.3321
---
Bring a dish. Music from noon.
```

The same shorthand habit as `location: "lat, lng"`. Every shape is a string a person
would type:

| `when:` | Meaning |
|---|---|
| `2026-10-04` | all day |
| `2026-10-04 19:00` | starts then; no end (feed emits a one-hour default, templates show start only) |
| `2026-10-04 19:00 to 21:00` | start and end |
| `2026-10-04 19:00 to 2026-10-05 02:00` | crosses midnight |
| `2026-10-04 to 2026-10-06` | multi-day, all day |
| `mondays 19:00 to 20:30` | starting next Monday, weekly, no end (sugar for `repeats: weekly`) |

Longer forms are flat keys beside `when`, so a form field and a front-matter key are
spelled the same:

```yaml
when: 2026-10-07 19:00
ends: 2026-10-07 20:30
timezone: America/Los_Angeles     # default: the site's [site] timezone
repeats: weekly                    # daily | weekly | monthly | yearly
except: [2026-11-25, 2026-12-23]   # skip these dates
```

`repeats` also takes a small map when the shorthand isn't enough:

```yaml
repeats:
  every: month             # day | week | month | year, or "2 weeks"
  on: first tuesday        # weekly: [tue, thu]; monthly: first tuesday | 15 | last friday
  until: 2027-06-30        # or count: 12
```

### A submission page (the events-board use case)

No new SDK piece. `<friendo-form>` already sends unknown names into `data`; the server
lifts `when` and friends out of it on create, exactly as it geo-tags a `{lat,lng}` today:

```html
<friendo-form collection="events">
  <input name="title" placeholder="What's happening?" required>
  <input name="when" type="datetime-local" required>
  <input name="ends" type="datetime-local">
  <select name="repeats">
    <option value="">Once</option><option>weekly</option><option>monthly</option>
  </select>
  <friendo-input name="body" type="richtext"></friendo-input>
  <friendo-input name="where" type="location"></friendo-input>
  <button type="submit">Submit event</button>
</friendo-form>
```

A native `datetime-local` yields `2026-10-04T19:00` with no zone; the server reads it
in the site's timezone. Members submit into the review queue behind the existing
`content.accept_submissions` setting; contributors publish directly. Phase 2 adds a
`<friendo-input type="when">` that bundles start/end/all-day/repeats for people who
want one control, but the native inputs are the zero-JS baseline.

### Listing, the event page, subscribing

```html
{# pages/events/index.html #}
<ul>
{% for e in collections.events|upcoming %}
  <li><a href="/events/{{ e.slug }}">{{ e.title }}</a> · {{ e.when|when }}</li>
{% empty %}
  <li>Nothing coming up.</li>
{% endfor %}
</ul>
<p><a href="/calendar.ics">Subscribe in your calendar app</a></p>
```

```html
{# pages/events/[slug].html #}
<h1>{{ record.title }}</h1>
<p>{{ record.when|when }}{% if record.when.repeats %} · {{ record.when.repeats }}{% endif %}</p>
{% if record.when.next %}<p>Next: {{ record.when.next|when }}</p>{% endif %}
{{ record.body|markdown }}
<friendo-map target-type="post" target-id="{{ record.id }}"></friendo-map>
<p>{{ record.rsvps.going }} going · {{ record.rsvps.maybe }} maybe</p>   {# phase 3, no JS #}
<friendo-rsvp post-id="{{ record.id }}"></friendo-rsvp>                  {# phase 3 #}
<a href="/calendar.ics?record={{ record.id }}">Add to my calendar</a>
```

A month grid with navigation is interactive by nature, so it's a component, like the
map (phase 2):

```html
<friendo-calendar collection="events"></friendo-calendar>
```

## Runtime deltas

### Schema — migration `0013_events.sql`

```sql
CREATE TABLE IF NOT EXISTS events (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL DEFAULT 'local',
    target_type TEXT NOT NULL DEFAULT 'post',
    target_id   TEXT NOT NULL DEFAULT '',
    starts      TEXT NOT NULL DEFAULT '',       -- local wall time, RFC3339 with offset
    ends        TEXT NOT NULL DEFAULT '',
    all_day     INTEGER NOT NULL DEFAULT 0,
    timezone    TEXT NOT NULL DEFAULT '',       -- IANA name; '' = site default at read time
    rrule       TEXT NOT NULL DEFAULT '',       -- RFC 5545 RRULE, '' = one-off
    exdates     TEXT NOT NULL DEFAULT '[]',     -- JSON list of skipped dates
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_events_target ON events(site_id, target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_events_starts ON events(site_id, starts);
```

One row is a **series**. A one-off is a series with no rule. `starts`/`ends` are the
*first* occurrence in local wall time with the zone's offset at that instant; the
zone name travels beside it so DST is re-derived per occurrence (a weekly 7pm stays
7pm across the change — storing UTC would drift it an hour). One row per post in this
release; the target key already allows more for later.

The table rides `push --data` / `pull --data` and the bundle export like `locations`
does (add it to the sync handlers' table list).

### Lifting — the one rule

**`when`, `ends`, `timezone`, `repeats`, `except` are reserved.** Wherever a post's
fields arrive, these are pulled out of `data` and reconciled into the post's `events`
row; the rest of `data` is untouched.

- **Importer** (`runtime/go/content`): beside `parseLocation`, a `parseWhen` that
  accepts the shapes above and returns a series. Delete the lifted keys from `meta`
  alongside `title/slug/status/date/location`. Reconcile with a deterministic id
  (`DeterministicEventID(recordID)`) so re-import is idempotent and removing the field
  deletes the row — the exact `UpsertLocation`/`DeleteLocation` dance.
- **API** (`handleCreateRecord` **and** `handleUpdateRecord`): an `autoSchedule`
  beside `autoGeotag`, run whenever `data` is present. Unlike geotag it must run on
  update too — an editor fixing a submitted event's time is the common case.
- **Parsing is one Go function**, `data.ParseWhen(meta map[string]any, siteTZ)`,
  shared by importer and API so the front-matter form and the form-submission form
  can't diverge. Table-tested.

### Timezone

- `[site] timezone = "America/Los_Angeles"` in `friendo.toml` (`siteConfig` +
  `exportConfig`). `friendo init` writes the machine's zone in. Unset = UTC, with a
  one-line startup notice so nobody is surprised.
- `import _ "time/tzdata"` in the binary so IANA zones resolve on a bare container.
- A per-post `timezone:` overrides the site's (a Zoom call announced in another zone).
- The existing `date` filter becomes zone-aware: it renders in the site zone by
  default, and a `when` value carries its own zone. `{{ x|date:"Jan 2" }}` keeps
  working unchanged for `created`/`published_at`.

### Template context

`record.when` (the post's series, or absent):

| Field | Value |
|---|---|
| `starts`, `ends` | RFC3339 strings in the event's zone (`ends` may be empty) |
| `all_day` | bool |
| `timezone` | IANA name |
| `repeats` | human text: `"weekly"`, `"every 2 weeks on Tue, Thu until Dec 31"`, or empty |
| `next` | the next occurrence at or after now (`{starts, ends}`), or empty if the series is over |
| `rule` | the raw RRULE (for the curious) |

Every record in `collections.<name>` also carries `when` — attached in
`buildCollectionsContext` with **one** `SELECT … FROM events WHERE site_id = ?` mapped
by `target_id`, not per-record queries. `attachRecordRelations` does the same for the
focused record (and, phase 3, adds `record.rsvps`).

**Filters** (registered in `renderer` beside `sort_by`):

| Filter | What |
|---|---|
| `upcoming` / `upcoming:12` | occurrences from now, soonest first, default window 12 months, optional limit |
| `past` / `past:12` | occurrences before now, most recent first |
| `in_month:"2026-10"` | occurrences in a month |
| `on_day:"2026-10-04"` | occurrences on a day |
| `when` | formats a `when` (or an occurrence) the way a person would: `Sat Oct 4, 10:00 am – 4:00 pm`; all-day: `Sat Oct 4`; multi-day: `Oct 4 – 6` |

`upcoming`/`past`/`in_month`/`on_day` **expand** series: a weekly meet-up yields one
entry per week, each a shallow copy of the record with `when.starts`/`when.ends` set to
that occurrence and `when.occurrence = true`. A list of *records* (`collections.events`)
therefore becomes a list of *occurrences* — that's what a calendar page wants, and
`{{ e.slug }}` still links to the one post. `sort_by:"when.starts"` also works on the
un-expanded list.

### Recurrence — phase 1

- **Library:** `github.com/teambition/rrule-go` — pure Go (the binary stays CGO-free),
  RFC 5545 RRULE/EXDATE, `Between(after, before)` expansion.
- **Shorthand → RRULE** (`data.ParseRepeats`):

| Author writes | RRULE |
|---|---|
| `repeats: weekly` | `FREQ=WEEKLY` |
| `when: mondays 19:00` | `FREQ=WEEKLY` with `starts` = next Monday |
| `repeats: {every: week, on: [tue, thu]}` | `FREQ=WEEKLY;BYDAY=TU,TH` |
| `repeats: {every: 2 weeks}` | `FREQ=WEEKLY;INTERVAL=2` |
| `repeats: {every: month, on: first tuesday}` | `FREQ=MONTHLY;BYDAY=1TU` |
| `repeats: {every: month, on: last friday}` | `FREQ=MONTHLY;BYDAY=-1FR` |
| `repeats: {every: month, on: 15}` | `FREQ=MONTHLY;BYMONTHDAY=15` |
| `… until: 2027-06-30` / `… count: 12` | `;UNTIL=…` / `;COUNT=12` |
| `except: [2026-11-25]` | `EXDATE` list |
| `rrule: "FREQ=…"` | escape hatch, stored verbatim |

- **Expansion window:** filters and `/calendar.json` expand at most 2 years ahead and
  cap at 1,000 occurrences per series; an unbounded weekly rule is fine because the
  window bounds it. The `.ics` feed does **not** expand — it emits `RRULE`/`EXDATE`
  and lets the subscriber's app do it (that's how every calendar app expects it).
- **DST:** expansion runs in the series' zone on wall-clock time, then each
  occurrence is stamped with that instant's offset. A 7pm weekly stays 7pm.
- **In phase 1:** skipping dates (`except`). **Not in phase 1:** moving a single
  occurrence to another time (iCalendar `RECURRENCE-ID` overrides). A skip plus a
  one-off post covers it until then.

### The feed — `/calendar.ics` and `/calendar.json`

Both are registered in `BuildSite` beside `/friendo.js`, *unless the site's `pages/`
defines that path* (`BuiltSite.HasPage`) — the same "a home-site page takes over" rule
as the apex default pages. Both are public and published-only.

Query params on either: `collection=events` (one collection), `record=<id>` (one post
— the "add to my calendar" link; with `occurrence=<start>` a single instance of a
recurring one), and on `.json` a `from`/`to` window.

`.ics` mapping, one `VEVENT` per series:

| iCalendar | From |
|---|---|
| `UID` | `<events.id>@friendo` — stable across hosts and custom domains |
| `DTSTART` / `DTEND` | `;TZID=<zone>:` local time; `;VALUE=DATE:` for all-day (end exclusive, +1 day); a missing `ends` → +1 hour |
| `RRULE` / `EXDATE` | verbatim from the row |
| `SUMMARY` | post title |
| `DESCRIPTION` | body rendered to plain text (markdown → text; first ~2000 chars), then the URL |
| `URL` | `permalinkResolver` (already exists for the map) |
| `LOCATION` / `GEO` | the post's `locations` row label / `lat;lng` — the pin and the calendar entry agree for free |
| `CATEGORIES` | collection name |
| `DTSTAMP`, `LAST-MODIFIED`, `SEQUENCE` | `updated` (SEQUENCE = unix seconds of `updated`, monotonic) |
| header | `PRODID:-//friendo//`, `X-WR-CALNAME:<site name>`, `X-WR-TIMEZONE`, and a `VTIMEZONE` per zone used (generated from Go's zone data; some clients ignore `TZID` without it) |

Headers: `Content-Type: text/calendar; charset=utf-8`, `Cache-Control: public,
max-age=900`, `ETag` from the newest `updated` in the set — subscribers poll every few
hours and a cheap 304 keeps a busy site cheap. Line folding at 75 octets and text
escaping per RFC 5545 — golden-file tested.

`/calendar.json` returns expanded occurrences for the SDK component:

```json
{ "events": [ { "id": "…", "series_id": "…", "collection": "events", "slug": "harvest-fair",
                "title": "Harvest Fair", "url": "/events/harvest-fair",
                "starts": "2026-10-04T10:00:00-07:00", "ends": "2026-10-04T16:00:00-07:00",
                "all_day": false, "timezone": "America/Los_Angeles",
                "repeats": "weekly" } ] }
```

**Visibility rule.** A collection whose dynamic page carries a gate tag or sits under an
`[access]` path (`renderer.FindGate` / `AccessRules.Requires` on the route's
`urlTemplate`) is left out of both feeds. The docs say so plainly: "members-only
events don't go in the public feed."

**Static export** writes `dist/calendar.ics` and `dist/calendar.json` from the same
code path (the export already knows the published records and the site name), so a
static site is subscribable and `<friendo-calendar>` works on a static host with no
API behind it — the same property `record.gallery` and `record.comments` have.

### Admin SPA — phase 2

The record editor edits only `title/slug/body/status` today and never touches `data`
(the API leaves `data` alone when omitted, so nothing is lost — but nothing is
editable either). Add:

- A **When** section on the record form: start, end, all-day, timezone (defaulting to
  the site's), and a **Repeats** control mirroring the shorthand (never/daily/weekly/
  monthly/yearly + on/until) plus skipped dates. Sent as the same reserved keys inside
  `data`; the API's lifting does the rest. Empty = not an event; the section is on
  every record, since any post may have a date.
- The **collection list** shows the next occurrence; the **review queue** shows `when`
  under the title (a submitted event's time is the first thing a reviewer checks).
- A **Where** section for `location` while we're there — the same gap.

### SDK — phase 2

- **`<friendo-calendar collection="events" view="month|list">`** — fetches
  `/calendar.json` for the visible window; month grid with prev/next, a list/agenda
  view, each entry linking to its post. Parts: `grid`, `day`, `today`, `event`, `nav`,
  `title`, `empty`. Dependency-free (a month grid is a few hundred lines of DOM), and
  it works from `dist/calendar.json` on a static host.
- **`<friendo-input name="when" type="when">`** — one control for start, end, all-day,
  and repeats; its `.value` is the flat `{when, ends, all_day, repeats}` shape, which
  `<friendo-form>` spreads into `data` (a small special case in the form's gather step,
  like `media`). Native inputs remain the no-JS baseline.

### RSVP — phase 3

Migration `0014_rsvps.sql`:

```sql
CREATE TABLE IF NOT EXISTS rsvps (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL DEFAULT 'local',
    event_id   TEXT NOT NULL DEFAULT '',        -- events.id (the series)
    occurrence TEXT NOT NULL DEFAULT '',        -- start of the instance answered for (a one-off uses its own start)
    author_id  TEXT NOT NULL DEFAULT '',        -- authors.id (persona), like comments
    answer     TEXT NOT NULL DEFAULT 'going',   -- going | not_going | maybe
    created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_rsvps_unique ON rsvps(event_id, occurrence, author_id);
CREATE INDEX IF NOT EXISTS idx_rsvps_event ON rsvps(site_id, event_id, occurrence, answer);
```

**Every RSVP names a time.** On a one-off that's invisible. On a weekly event, "are you
coming?" means *this* Monday, so the answer is stored against that occurrence and the
component asks about the next one by default (with a way to pick another upcoming
one). The server never trusts a submitted time: it expands the series over the window
and rejects an occurrence that isn't on the list.

- *Built:* the API hangs off the **post** id (`/_/api/posts/{id}/rsvps`, like comments),
  since that's what a template has; the series id stays internal.
- **API** (self-gating like reactions): `GET /_/api/events/{id}/rsvps?occurrence=…` —
  public counts (`going`, `not_going`, `maybe`) plus `mine`; names only for the post's
  author or a moderator (`comment.moderate.own/any` reused as the organizer test).
  `POST` (member) `{occurrence, answer}` — upserts, so changing your mind is the same
  call. `DELETE` (own) withdraws. Rate-limited with the comment bucket.
- **Component:** `<friendo-rsvp post-id>` — three buttons with `aria-pressed` on yours,
  the counts, and for recurring events the occurrence it's asking about. Signed out it
  fires `friendo:needs-auth` like reactions. Parts: `question`, `when`, `button`,
  `count`, `status`.
- **SSR:** `record.rsvps` → `{going, not_going, maybe}` for the next occurrence, so a
  page can say "12 going" with no JS. Names stay behind the API.
- **Admin:** an Attendees list per event (per occurrence for recurring) with a CSV link.
- **Edits:** if a series' time moves, RSVPs whose occurrence falls on the same calendar
  day as a new occurrence are re-keyed to it; ones that no longer match any occurrence
  (weekday changed, date skipped) are kept and shown to the organizer as *no longer
  scheduled*. Phase 3 has no email, so notifying them is a later step.
- **Not in 3:** email confirmations/reminders (needs the email seam extended beyond
  OTP), guests-per-RSVP counts, capacity.

## Auth & moderation — nothing new

Contributors+ create events directly; members submit under `content.accept_submissions`
and land in the queue as `pending`. Publishing an event is publishing a post. Editing
its time is editing a post (`content.edit.own/any`). The feed shows published only.
RSVPs are member acts with the comment rate limit. Organizer = the post's author or a
moderator, the same ownership axis comments use.

## Test coverage

- **`data.ParseWhen` / `ParseRepeats`** — a table test over every shorthand row above,
  including bad input ("next Tuesday", 25:00, `to` before start → warning, not a row).
- **Expansion** — fixtures across a DST boundary (7pm weekly stays 7pm), `until` vs
  `count`, `except`, a `last friday` monthly, window caps.
- **`.ics` golden files** — one-off, all-day, multi-day, recurring with EXDATE, a pin
  (LOCATION/GEO), text escaping and 75-octet folding. Feed bugs otherwise surface in
  someone else's calendar app a day later.
- **REST scenarios** (`scenarios.json`): create with `when` → row exists and `data` has
  no `when`; update moves it; remove clears it; member submission with `when` is
  pending; a gated collection is absent from `/calendar.ics`; `?record=` returns one
  VEVENT; ETag → 304.
- **Render scenarios**: `upcoming` expands and orders; `when` filter formats; `in_month`.
- **Export test**: `dist/calendar.ics` + `dist/calendar.json` written, published only.
- **Content import**: idempotent re-import; removing `when` deletes the row.
- **Playwright** (phases 2–3): `sdk-calendar.mjs` (grid renders, prev/next, click →
  post), `sdk-rsvp.mjs` (sign in, answer, count, change answer, a recurring event
  asks about the next occurrence; an invalid occurrence is refused).

## Phasing (each independently shippable)

1. **Core, with recurrence.** Migration `0013`, `ParseWhen`/`ParseRepeats`, lifting in
   importer + create + update, `[site] timezone` + `tzdata`, zone-aware `date`,
   `record.when` + collection attach, the five filters, `/calendar.ics` +
   `/calendar.json` with the visibility rule, static export, sync tables, `friendo init`
   gets a `pages/events/` example. **This alone is the events board with a subscribable,
   recurring calendar**, submissions via native inputs in `<friendo-form>`.
2. **Interactive + admin.** `<friendo-calendar>`, `<friendo-input type="when">`, the
   admin When/Where sections, dates in the list and review queue.
3. **RSVP.** Migration `0014`, the rsvps API, `<friendo-rsvp>`, `record.rsvps`, the
   Attendees view + CSV.

## Open items / out of scope (first cut)

- **Single-occurrence overrides** (`RECURRENCE-ID`) — after phase 1; `except` + a
  one-off covers it.
- **Personal feeds** for members-only events, and a "my RSVPs" feed — a per-member
  token URL, later.
- **Reminders / confirmation email** — needs the email seam generalized.
- **A `[year]/[month]` page route** — the route table treats the parent folder as a
  collection, so it doesn't fit; `<friendo-calendar>` (or `?month=` read in a template)
  covers navigation.
- **`request.host` in the template context** — handy for a `webcal://` link; trivial,
  add with phase 1 if wanted.

## Deferred: slots (resource booking, volunteer shifts)

Designed on the same day and cut from this release. Recorded so the later work — likely
its own "booking + sign-ups" release — starts from what was worked out.

- **What it was.** A post could carry `slots:`, a list of claimable time windows, each
  with `when`, an optional `label`, and a `capacity` (0 = unlimited). Each slot would be
  another `events` row (a `kind` column: `when` | `slot`, plus `label`, `capacity`,
  `position`), which is why the sidecar table is keyed by target. Capacity 1 per slot
  is a booking form; capacity *n* is a volunteer shift; a full slot takes a waitlist
  and a cancellation promotes the first person waiting.
- **Recurring slots** ("4 spots every Monday, forever") come free at the storage level
  — a slot row has `rrule` like any series — but force sign-ups to name an occurrence
  (the same `occurrence` column RSVP now uses), with capacity counted per occurrence,
  server-side validation of the named occurrence, a display window (`weeks` on a
  `<friendo-slots>` component; `record.slots|upcoming:8` server-side), and `when:
  mondays 09:00 to 10:00` sugar.
- **What made it heavy.** Deterministic slot ids keyed on label-or-position, so
  reordering unlabelled slots strands sign-ups; re-keying by calendar day when a series'
  time moves; orphaned sign-ups when a weekday changes or a date is skipped, with no
  email to tell anyone; and slot rows flooding the public feed (they'd be excluded by
  default, with a per-booking "add to my calendar" VEVENT instead).
- **Where to pick up.** The RSVP table already carries the per-occurrence keying and
  the organizer view; slots add capacity, waitlist, and the slot rows. A `slots:` sugar
  like `every 30 minutes from 9 to 5` was also floated.
