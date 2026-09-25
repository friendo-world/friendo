---
title: Calendar & events
slug: calendar
group: Concepts
weight: 11
---

An **event is a post with a `when`**. Give any post a time and it becomes an
event: it shows up in your listings sorted by date, it has a page like any other
post, and it goes out in your site's calendar feed, which anyone can subscribe
to from Google Calendar, Apple Calendar or Outlook. Repeating events are built
in.

## Write an event

In a content file, add a `when:` line to the front matter:

```yaml
---
title: Harvest Fair
when: 2026-10-04 10:00 to 16:00
location: 47.6062, -122.3321
---
Bring a dish. Music from noon.
```

Every shape is something you'd type by hand:

| `when:` | Meaning |
|---|---|
| `2026-10-04` | all day |
| `2026-10-04 19:00` | starts at 7 pm (no end) |
| `2026-10-04 19:00 to 21:00` | 7 to 9 pm |
| `2026-10-04 22:00 to 02:00` | crosses midnight |
| `2026-10-04 19:00 to 2026-10-05 02:00` | the same, spelled out |
| `2026-10-04 to 2026-10-06` | three days, all day |
| `mondays 19:00 to 20:30` | every Monday from now on |

Times are read in your site's timezone — set it once in
[`friendo.toml`](/docs/config): `timezone = "America/Los_Angeles"`. A post can
say `timezone:` itself when one event is somewhere else.

The other keys sit beside `when`:

```yaml
when: 2026-10-07 19:00
ends: 20:30                        # or a full date-time
timezone: Europe/Paris             # this event only
repeats: weekly                    # daily | weekly | monthly | yearly | every 2 weeks
except: [2026-11-25, 2026-12-23]   # skip these dates
```

For more control, `repeats` takes a small map:

```yaml
repeats:
  every: month            # day | week | month | year, or "2 weeks"
  on: first tuesday       # weekly: [tue, thu]; monthly: first tuesday | last friday | 15
  until: 2027-06-30       # or count: 12
```

The `date:` key is still the post's published date, not the event's time. If
you already know iCalendar, `rrule: "FREQ=WEEKLY;BYDAY=TU,TH"` is accepted
verbatim.

## List events

Every record carries `when` (empty for a post with no time). The `upcoming`
filter gives the events still to come, soonest first, and **expands repeating
events** into one entry per date:

```html
{% for e in collections.events|upcoming %}
  <li><a href="/events/{{ e.slug }}">{{ e.title }}</a> · {{ e.when|when }}</li>
{% empty %}
  <li>Nothing coming up.</li>
{% endfor %}
```

| Filter | What |
|---|---|
| `upcoming` / `upcoming:5` | occurrences from now (the next twelve months), soonest first, optionally at most 5 |
| `past` / `past:5` | occurrences already started, most recent first |
| `in_month:"2026-10"` | occurrences in a month (this month if you leave it blank) |
| `on_day:"2026-10-04"` | occurrences on a day |
| `when` | formats a time the way a person would: `Sat Oct 4, 10 am – 4 pm`, `Sat Oct 4` (all day), `Oct 4 – 6` |
| `google_calendar_url` | `{{ record\|google_calendar_url }}` — Google Calendar's pre-filled "add event" link (see [below](#google-calendar-apple-calendar-outlook)) |

`when` takes a Go date layout if you want the date part your way:
`{{ e.when|when:"2 January" }}`.

An expanded entry is the post with `when` set to that date, so `{{ e.slug }}`
still links to the one post. `sort_by:"when.starts"` orders the unexpanded list.

## The event page

On a dynamic route, `record.when` is:

| Field | Value |
|---|---|
| `starts`, `ends` | RFC 3339 times in the event's zone (`ends` may be empty) |
| `all_day` | true for an all-day event |
| `timezone` | the zone name |
| `repeats` | `"weekly"`, `"monthly on the first Tuesday"`, `"every 2 weeks on Tue, Thu until Dec 31, 2026"` — or empty |
| `next` | the next occurrence (`starts`, `ends`), or empty when the series is over |
| `except` | the skipped dates |

```html
<h1>{{ record.title }}</h1>
<p>{{ record.when|when }}{% if record.when.repeats %} · {{ record.when.repeats }}{% endif %}</p>
{% if record.when.next %}<p>Next: {{ record.when.next|when }}</p>{% endif %}
<a href="/calendar.ics?record={{ record.id }}">Add to my calendar</a>
```

## Subscribe: `/calendar.ics`

Every site serves its published events at **`/calendar.ics`**. Paste that URL
into Google Calendar (*Other calendars → From URL*), Apple Calendar (*File →
New Calendar Subscription*) or Outlook, and the calendar stays in sync as you add
and edit events — repeating events included, since the feed carries the rule and
the calendar app does the repeating.

```html
<a href="/calendar.ics">Subscribe in your calendar app</a>
```

| URL | What |
|---|---|
| `/calendar.ics` | every published event |
| `/calendar.ics?collection=events` | one collection |
| `/calendar.ics?record=<id>` | one event — an "add to my calendar" link |
| `/calendar.ics?record=<id>&occurrence=<start>` | one date of a repeating event |
| `/calendar.json` | the same events as JSON, one entry per date, for scripts and the calendar component (`?from=2026-10-01&to=2026-11-01`) |

Each entry carries the post's title, its body as plain text, a link back to
its page, and — if the post has a [location](/docs/maps) — the place and its
coordinates. Only **published** posts go out. A collection whose page is
[members-only](/docs/templates#members-only-pages) is left out of the feed
entirely, since the feed is public. If you'd rather serve something else at
`/calendar.ics`, a page of yours at that path wins.

### Google Calendar, Apple Calendar, Outlook

A link to `/calendar.ics` downloads a file, which is what Apple Calendar and
desktop Outlook want but not what Google Calendar wants. Every page has a
`calendar` variable with a link for each:

```html
Subscribe: <a href="{{ calendar.google }}">Google Calendar</a> ·
<a href="{{ calendar.webcal }}">Apple Calendar / Outlook</a> ·
<a href="{{ calendar.ics }}">feed address</a>
```

| Variable | What |
|---|---|
| `calendar.google` | Google Calendar's "add this calendar" page, pointed at your feed |
| `calendar.webcal` | a `webcal://` address — opens Apple Calendar, Outlook and most desktop apps with a subscribe prompt |
| `calendar.ics` / `calendar.json` | the feed's own address, to paste into any app |

For **one event**, the `google_calendar_url` filter builds Google's pre-filled
"create event" form from the post — title, time in its zone, the place from its
pin, and the repeat rule:

```html
<a href="{{ record|google_calendar_url }}">Add to Google Calendar</a> ·
<a href="/calendar.ics?record={{ record.id }}">Download .ics</a>
```

Or let one tag offer all of them as a menu (it needs `friendo.js`):

```html
<friendo-add-to-calendar post-id="{{ record.id }}"></friendo-add-to-calendar>
<friendo-add-to-calendar subscribe collection="events"></friendo-add-to-calendar>
```

With `post-id` the menu is Google Calendar, Apple Calendar, Outlook and a
download for that event (`occurrence` picks one date of a repeating one). With
`subscribe` it's the whole feed, plus "copy the feed address". Parts: `button`,
`menu`, `item`, `copy`, `status`, `error`.

Two things to know about Google: it fetches the feed from its own servers, so
subscribing only works once the site is online (not on `localhost`), and it
refreshes subscribed feeds slowly — often once or twice a day. That's Google's
pace, not yours; Apple Calendar and Outlook let the reader pick.

A [static export](/docs/static-export) writes `calendar.ics` and
`calendar.json` into `dist/`, so a static site is subscribable too. The
`calendar.*` links there come from `[deploy] domain` (or `target`) in
`friendo.toml`, since a file has no idea where it will be served from.

## A month view: `<friendo-calendar>`

For a grid with navigation, drop in one tag (it reads `/calendar.json`, which a
static export writes too):

```html
<friendo-calendar collection="events"></friendo-calendar>
<script src="/friendo.js" defer></script>
```

| Attribute | What |
|---|---|
| `collection` | one collection (default: every event) |
| `view` | `month` (default) or `list` — the upcoming dates grouped by day |
| `month` | the month to open on, `YYYY-MM` (default: this month) |
| `limit` | in `list` view, how many dates to show |

Every entry links to its post. Style it with `::part()` — the parts are `nav`,
`title`, `button`, `view`, `grid`, `weekday`, `day`, `today`, `outside`, `date`,
`event`, `more`, `list`, `group`, `heading`, `time`, `empty` and `error`:

```css
friendo-calendar::part(event) { background: #efeaff; }
friendo-calendar::part(today) { outline: 2px solid rebeccapurple; }
```

## RSVP: `<friendo-rsvp>`

Ask "are you coming?" on an event page. Signed-in members answer **Going**,
**Maybe** or **Can't go**; the tally is public; one answer per person, and
changing your mind replaces it. On a repeating event the question is about the
**next date** (or the one an `occurrence` attribute names), so a weekly meet-up
asks about this week.

```html
<p>{{ record.rsvps.going }} going · {{ record.rsvps.maybe }} maybe</p>   {# no JS needed #}
<friendo-rsvp post-id="{{ record.id }}"></friendo-rsvp>
```

| Attribute | What |
|---|---|
| `post-id` | the post (it must have a `when`) |
| `occurrence` | an RFC 3339 start, to ask about a particular date of a repeating event |
| `names` | also list who answered — shown only to the post's author and moderators |
| `question`, `going-label`, `maybe-label`, `not-going-label` | your own wording |

Parts: `question`, `when`, `row`, `button`, `count`, `mine`, `names`, `name`,
`status`, `signed-out`, `error`. The button for the viewer's own answer carries
`aria-pressed="true"`. Signed-out clicks fire `friendo:needs-auth`, like reactions;
an answer fires `friendo:rsvp`.

`record.rsvps` is the tally for the next date, server-rendered: `going`,
`maybe`, `not_going` and `occurrence`.

**Who answered.** In the admin, a record with a time has an **Attendees** link:
every answer grouped by date, with a CSV download. The API is
`GET /_/api/posts/<id>/attendees` (`?format=csv`), for the post's author or a
moderator. If you move an event's time, answers follow it to the new time on the
same day; answers for a date the event no longer happens on are kept and marked
*no longer scheduled*.

## In the admin

Every record's form has a **When** section — start, end, all day, repeats (with
an until date and dates to skip), timezone — and a **Where** section for a map pin.
Leave When empty and the post is an ordinary post; clear it to remove the event.
The collection list shows each record's time, and the review queue shows a
submitted event's date.

## Let people submit events

A [`<friendo-form>`](/docs/community#submitting-posts-from-a-page) with a
`when` field makes an event. The field names are the same as the front-matter
keys, so an ordinary date input is all it takes:

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

For one control with start, end, all-day and repeats together, use
`<friendo-input name="when" type="when">` in place of the three native fields.

Contributors publish directly. With **Members can submit posts** on, a member's
event waits in the review queue like any submission, with its date shown, until
an editor approves it. The API works the same way: `POST /_/api/collections/events/records`
with `"data": {"when": "2026-10-04 19:00"}` — the runtime lifts `when` out of
`data` into the event, and the record comes back with `when` filled in (and out
of `data`). An update that says nothing about `when` leaves the event alone, so
reading a record and writing it back is safe; send `"when": null` to remove it.

## Where it lives

A post's time is one row in the site's `events` table, alongside the post (the
same way a `location` becomes a pin). It travels on `friendo push --data` and
`pull`, and it's removed when the post is deleted or its `when` line is taken
away. Dates in the feed and on pages are computed from the rule each time, so
editing a repeating event in one place changes every date at once.
