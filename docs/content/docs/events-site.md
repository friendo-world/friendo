---
title: Build an events site
slug: events-site
section: Tutorials
weight: 40
description: Add a weekly event, list what's coming up, show a month calendar, take RSVPs and publish a feed people can subscribe to. About twenty minutes.
---

In this tutorial you build the events side of a site: a repeating **event**, a
list of what's coming up, a month calendar, RSVPs, and a calendar feed that
Google Calendar, Apple Calendar and Outlook can subscribe to. You need a site from
[Your first site](/docs/first-site) running with `friendo serve`. It helps to have
done [Add sign-in and comments](/docs/add-a-community) first, since RSVPs need a
signed-in member.

## 1. Look at what's already there

A fresh `friendo init` site already has an events collection:

| File | What it is |
|---|---|
| `content/events/first-meetup.md` | one event that repeats weekly |
| `pages/events/index.html` | the list at `/events/` |
| `pages/events/[slug].html` | one page per event |

Open `content/events/first-meetup.md`. Its front matter is the whole trick:

```yaml
---
title: First meetup
when:
  start: 2026-10-20 19:00 to 20:30
  repeats: weekly
---
```

An event is a post with a **`when`**. Nothing else makes it one. Open
`http://localhost:3000/events/` and you see *First meetup* once for every week
ahead.

Times are read in the site's timezone, set in `friendo.toml`:

```toml
[site]
name = "my-site"
timezone = "America/Chicago"
```

Change it to your own zone (an IANA name like `Europe/Paris`) and restart
`friendo serve`.

## 2. Add a weekly event

Make a new file, `content/events/book-club.md`:

```markdown
---
title: Book club
when: thursdays 18:30 to 20:00
---
This month: whatever's on the library's front table. Come even if you didn't finish it.
```

`thursdays 18:30 to 20:00` means every Thursday from now on, 6:30 to 8 pm. For
one-off events write a date instead: `2026-10-04 19:00 to 21:00`, or
`2026-10-04` for all day. Every shape is in [Calendar](/docs/calendar).

Save and reload `/events/`. Book club now appears every Thursday, mixed in with
the meetups by date.

## 3. Keep the list short

The list comes from this loop in `pages/events/index.html`:

```html
{% for e in collections.events|upcoming %}
```

The `upcoming` filter gives the dates still to come, soonest first, and expands
a repeating event into **one entry per date** for the next twelve months. With
two weekly events that's about a hundred entries. Cap it:

```diff
-{% for e in collections.events|upcoming %}
+{% for e in collections.events|upcoming:6 %}
```

Reload. You see the next six dates, for example:

```
Book club      Thu Oct 1, 6:30 pm – 8 pm · weekly
Book club      Thu Oct 8, 6:30 pm – 8 pm · weekly
Book club      Thu Oct 15, 6:30 pm – 8 pm · weekly
First meetup   Tue Oct 20, 7 pm – 8:30 pm · weekly
…
```

Each entry is the same post with `when` set to that date, so `{{ e.slug }}`
still links to the one Book club page.

## 4. Read the event page

Click *Book club*. The page is `pages/events/[slug].html`, where the event is
`event`:

```html
<h1>{{ event.title }}</h1>
<p>{{ event.when|when }}{% if event.when.repeats %} &middot; {{ event.when.repeats }}{% endif %}</p>
{% if event.when.repeats and event.when.next %}<p>Next: {{ event.when.next|when }}</p>{% endif %}
```

`event.when` prints the time in words (*Thu Oct 1, 6:30 pm – 8 pm*).
`event.when.repeats` is the rule in words (*weekly*) and empty for a one-off
event, so the `if` hides it. `event.when.next` is the next date of the series.
Every field is listed in [Calendar](/docs/calendar#the-event-page).

## 5. Take RSVPs

Add the tally and the RSVP tag under the body in `pages/events/[slug].html`:

```diff
     <div>{{ event.body|markdown }}</div>
+
+    <p>{{ event.rsvps.going }} going &middot; {{ event.rsvps.maybe }} maybe</p>
+    <friendo-rsvp post-id="{{ event.id }}"></friendo-rsvp>
+
```

Reload. You see *0 going · 0 maybe*, then *Are you coming?* with the next date
and three buttons: **Going**, **Maybe**, **Can't go**. Signed out, it says
*Sign in to answer.*

Sign in with the box in the nav (the code shows in the form locally) and press
**Going**. The button stays pressed and its count goes to 1. Reload and the
server-rendered line reads *1 going · 0 maybe*.

On a repeating event the question is about the next date, so a weekly club asks
about this week. One answer per member; pressing another replaces it.

## 6. Add a month calendar

In `pages/events/index.html`, put a calendar above the list:

```diff
 <h1>Events</h1>
+
+<friendo-calendar collection="events"></friendo-calendar>
+
+<h2>Coming up</h2>
```

Reload `/events/`. A month grid opens on this month with each event on its day.
The **‹** and **›** buttons move a month; **List** switches to the upcoming dates
grouped by day. Every entry links to its event page.

## 7. Let people subscribe

The bottom of `pages/events/index.html` already has subscribe links:

```html
<p>Subscribe: <a href="{{ calendar.google }}">Google Calendar</a> &middot;
<a href="{{ calendar.webcal }}">Apple Calendar / Outlook</a> &middot;
<a href="{{ calendar.ics }}">feed address</a></p>
```

`calendar` is on every page, with one link per calendar app, all pointing at
your site's feed. For a single button with a menu instead, add the tag after
them:

```diff
 <a href="{{ calendar.ics }}">feed address</a></p>
+<friendo-add-to-calendar subscribe collection="events"></friendo-add-to-calendar>
```

Reload and press **Subscribe ▾**: *Google Calendar*, *Apple Calendar*,
*Outlook* and *Copy the feed address*.

> **Note:** Google Calendar fetches the feed from its own servers, so subscribing
> with Google only works once the site is online. Apple Calendar and Outlook can
> try it now.

## 8. Check the feed

The feed lives at `/calendar.ics`. In a second terminal:

```bash
curl -s http://localhost:3000/calendar.ics | grep -E '^(SUMMARY|RRULE)'
```

You see each event once, with its rule:

```
RRULE:FREQ=WEEKLY
SUMMARY:Book club
RRULE:FREQ=WEEKLY
SUMMARY:First meetup
```

The feed carries the rule, not a hundred dates; the calendar app does the
repeating. Edit `book-club.md`, and every subscriber's calendar follows the next
time it refreshes.

> **What you built.** An events site from plain files: a weekly event written in
> one line, a short list of what's next, a month calendar, RSVPs with a tally the
> server prints, and a public feed any calendar app can subscribe to.

**Next:** [Deploy](/docs/deploy)
