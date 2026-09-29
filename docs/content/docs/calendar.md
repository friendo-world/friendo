---
title: Calendar & events
slug: calendar
section: Concepts
weight: 11
---

An **event is a post with a `when`**. Give any post a time and it becomes an
event: it shows up in listings sorted by date, it has a page like any other post,
and it goes out in your site's calendar feed, which anyone can subscribe to from
Google Calendar, Apple Calendar or Outlook. Repeating events are built in.

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
| `2026-10-04 to 2026-10-06` | three days, all day |
| `mondays 19:00 to 20:30` | every Monday from now on |

When there's more to say, `when` is a map:

```yaml
when:
  start: 2026-10-07 19:00        # the same shorthands work here
  end: 20:30                     # or a full date-time
  timezone: Europe/Paris         # this event only
  repeats: weekly                # daily | weekly | monthly | yearly | every 2 weeks
  except: [2026-11-25, 2026-12-23]
```

For more control, `repeats` takes a small map, and if you already know iCalendar,
`rrule: "FREQ=WEEKLY;BYDAY=TU,TH"` is accepted verbatim:

```yaml
when:
  start: 2026-10-06 19:00
  repeats:
    every: month            # day | week | month | year, or "2 weeks"
    on: first tuesday       # weekly: [tue, thu]; monthly: first tuesday | last friday | 15
    until: 2027-06-30       # or count: 12
```

Times are read in your site's timezone, set once in [friendo.toml](/docs/config):
`timezone = "America/Los_Angeles"`. The `date:` key is still the post's published
date, not the event's time.

## List events

Every post carries `when` (empty for a post with no time). The `upcoming` filter
gives the events still to come, soonest first, and **expands repeating events**
into one entry per date:

```html
{% for e in collections.events|upcoming %}
  <li><a href="/events/{{ e.slug }}">{{ e.title }}</a> · {{ e.when }}</li>
{% empty %}
  <li>Nothing coming up.</li>
{% endfor %}
```

| Filter | What |
|---|---|
| `upcoming` / `upcoming:5` | dates from now (the next twelve months), soonest first, at most 5 |
| `past` / `past:5` | dates already started, most recent first |
| `in_month:"2026-10"` | dates in a month (this month if blank) |
| `on_day:"2026-10-04"` | dates on a day |
| `when:"2 January"` | the time with your own date layout; `{{ e.when }}` alone reads `Sat Oct 4, 10 am – 4 pm` |
| `google_calendar_url` | `{{ e\|google_calendar_url }}`, Google's pre-filled "add event" link |

An expanded entry is the post with `when` set to that date, so `{{ e.slug }}` still
links to the one post. `sort_by:"when.start"` orders the unexpanded list.

## The event page

On an event's page the event is `event` (and also `post`). `event.when` is:

| Field | Value |
|---|---|
| `start`, `end` | RFC 3339 times in the event's zone (`end` may be empty) |
| `all_day` | true for an all-day event |
| `timezone` | the zone name |
| `repeats` | `"weekly"`, `"monthly on the first Tuesday"`, `"every 2 weeks on Tue, Thu until Dec 31, 2026"`, or empty |
| `next` | the next date (`start`, `end`), or empty when the series is over |
| `except` | the skipped dates |

```html
<h1>{{ event.title }}</h1>
<p>{{ event.when }}{% if event.when.repeats %} · {{ event.when.repeats }}{% endif %}</p>
{% if event.when.next %}<p>Next: {{ event.when.next }}</p>{% endif %}
<a href="/calendar.ics?post={{ event.id }}">Add to my calendar</a>
```

## Subscribe: `/calendar.ics`

Every site serves its published events at **`/calendar.ics`**. Paste that URL
into Google Calendar (*Other calendars → From URL*), Apple Calendar (*File → New
Calendar Subscription*) or Outlook, and the calendar stays in sync as you add and
edit events, repeating ones included: the feed carries the rule and the calendar
app does the repeating.

| URL | What |
|---|---|
| `/calendar.ics` | every published event |
| `/calendar.ics?collection=events` | one collection |
| `/calendar.ics?group=board` | one group's events |
| `/calendar.ics?post=<id>` | one event: an "add to my calendar" link |
| `/calendar.ics?post=<id>&date=<start>` | one date of a repeating event |
| `/calendar.json` | the same events as JSON, one entry per date (`?from=2026-10-01&to=2026-11-01`) |

Each entry carries the post's title, its body as plain text, a link back to its
page, and its [location](/docs/locations) as the place. Only **published** posts
go out, and a collection whose page is [members-only](/docs/templates#members-only-pages)
is left out, since the feed is public. A page of yours at `/calendar.ics` wins.

### Google Calendar, Apple Calendar, Outlook

A link to `/calendar.ics` downloads a file, which Apple Calendar and desktop
Outlook want but Google Calendar doesn't. Every page has a `calendar` variable
with a link for each:

```html
Subscribe: <a href="{{ calendar.google }}">Google Calendar</a> ·
<a href="{{ calendar.webcal }}">Apple Calendar / Outlook</a> ·
<a href="{{ calendar.ics }}">feed address</a>
```

Or let one tag offer all of them as a menu (it needs `friendo.js`):

```html
<friendo-add-to-calendar post-id="{{ event.id }}"></friendo-add-to-calendar>
<friendo-add-to-calendar subscribe collection="events"></friendo-add-to-calendar>
```

With `post-id` the menu adds that event (`date` picks one date of a repeating
one). With `subscribe` it's the whole feed, plus "copy the feed address".

Two things to know about Google: it fetches the feed from its own servers, so
subscribing only works once the site is online, and it refreshes subscribed feeds
slowly, often once or twice a day. Apple Calendar and Outlook let the reader pick.

A [static export](/docs/static-export) writes `calendar.ics` and `calendar.json`
into `dist/`, so a static site is subscribable too.

## Your private calendar

The public feed can only carry what everyone may see. A signed-in member has more:
events in members-only collections they can open, and their groups' events. A
calendar app can't sign in, so a member's feed address carries a **secret token**:

```
https://my-site.friendo.world/calendar.ics?token=…
```

`<friendo-add-to-calendar subscribe>` shows it to whoever is signed in: a *Your
calendar* menu with Google Calendar, Apple Calendar, Outlook and a copy button,
beside a *Reset link*. The feed is computed as that member. Add `mine` to the tag
for just the events they answered *going* or *maybe* to, or were invited to;
`group="board"` for one group's.

**The link is a secret.** Anyone holding it reads that member's calendar. *Reset
link* mints a new one and the old address stops working; calendar apps using it
must be re-added. `/calendar.json` follows the same rule and also honours the
session cookie, so a signed-in member's `<friendo-calendar>` shows their private
events with no token at all.

## A month view: `<friendo-calendar>`

```html
<friendo-calendar collection="events"></friendo-calendar>
```

`collection` narrows it (default: every event); `view` is `month` (default) or
`list`, the upcoming dates grouped by day; `month` is the month to open on
(`YYYY-MM`); `limit` caps a list. Every entry links to its post.

## RSVP

Ask "are you coming?" on an event page. Members answer **Going**, **Maybe** or
**Can't go**; the tally is public; one answer per person, and changing your mind
replaces it. On a repeating event the question is about the **next date** (or the
one a `date` attribute names), so a weekly meet-up asks about this week.

```html
<p>{{ event.rsvps.going }} going · {{ event.rsvps.maybe }} maybe</p>   {# no JS needed #}
<friendo-rsvp post-id="{{ event.id }}" names></friendo-rsvp>
```

`names` also lists who answered, shown only to the event's author and to
moderators. `event.rsvps` is the tally for the next date, server-rendered:
`going`, `maybe`, `not_going`, `invited` and `date`.

**Inviting people.** The organizer (the event's author, or a moderator) can ask
people to come. Each gets an RSVP waiting for their answer (*You're invited — are
you coming?* on the event page) and a note in their [inbox](/docs/community#notifications):

```html
<friendo-invite post-id="{{ event.id }}"></friendo-invite>
```

It paints only for the organizer: a box for profile names (`pat, sam`), a group
whose members to ask, and *my followers*. Inviting yourself does nothing.

**Who answered.** In the admin, an event has an **RSVPs** button: every answer
grouped by date, with a CSV download and the same invite box. If you move an
event's time, answers follow it to the new time on the same day; answers for a
date the event no longer happens on are kept and marked *no longer scheduled*.

## In the admin

Every post's form has a **When** section (start, end, all day, repeats with an
until date and dates to skip, timezone) and a **Location** section. Leave When
empty and the post is an ordinary post; clear it to remove the event. The
collection list shows each post's time, and the review queue shows a waiting
event's date.

## Let people submit events

A [`<friendo-form>`](/docs/community#posting-from-a-page) with a `when` field
makes an event. An ordinary date input is all it takes; dotted names fill in the
rest of the map:

```html
<friendo-form collection="events">
  <input name="title" placeholder="What's happening?" required>
  <input name="when" type="datetime-local" required>
  <input name="when.end" type="datetime-local">
  <select name="when.repeats">
    <option value="">Once</option><option>weekly</option><option>monthly</option>
  </select>
  <friendo-input name="body" type="richtext"></friendo-input>
  <friendo-input name="where" type="location"></friendo-input>
  <button type="submit">Submit event</button>
</friendo-form>
```

For one control with start, end, all-day and repeats together, use
`<friendo-input name="when" type="when">` in place of the native fields.

Contributors publish directly. With **Members can post** on, a member's event
waits for review like any post, with its date shown, until a moderator approves
it. The [API](/docs/api#posts) works the same way: send `"when"` inside `fields`,
as a string or a map, and it comes back on the post as `when`. An update that says
nothing about `when` leaves the event alone; send `"when": null` to remove it.

## Where it lives

A post's time is one row in the site's `events` table, beside the post, the same
way a location is. It travels on `friendo push --posts` and `pull`, and it's
removed when the post is deleted or its `when` is taken away. Dates in the feed
and on pages are computed from the rule each time, so editing a repeating event
in one place changes every date at once.
