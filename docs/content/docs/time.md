---
title: Events and time
slug: time
section: Concepts
weight: 90
description: An event is a post with a when, read in the site's timezone, with repeats kept as a rule and dates worked out each time.
---

friendo has no separate kind of thing called an event. An **event is a post with
a `when`**. Give any post a time and it becomes one: it shows up in listings by
date, it has a page like any other post, and it goes out in the site's calendar
feed. Take the time away and it's an ordinary post again. The same idea runs
through friendo: a [group](/docs/groups) is a post with members, and any post can
carry a [location](/docs/locations).

Because an event is a post, everything a post has, an event has too: a title, a
body, fields, an author, comments, a group it's filed under. And everything that
works on posts works on events: collections, members-only pages, review, push and
pull. The time is one more thing the post carries.

## Times are read in the site's timezone

A `when` like `2026-10-04 19:00` is written the way a person would say it, with
no zone attached. friendo reads it in the site's timezone, set once as `timezone`
under `[site]` in [friendo.toml](/docs/config). `friendo init` fills in your
machine's; with none set, it's UTC. So `19:00` means seven in the evening where
the site is, not where the server happens to run.

One event can name its own zone instead, for the meet-up held in another city.
Either way the event keeps its zone, and templates print its times in that zone.
Month and day boundaries, for "what's on in October" or "what's on today", are
read in the events' zone too.

## Repeats are a rule, not a list of dates

A weekly meet-up is not stored as fifty-two posts. It's one post with a rule:
"weekly", "monthly on the first Tuesday", "every 2 weeks until June". Skipped
dates are kept beside the rule as exceptions.

The dates themselves are worked out from the rule each time they're needed: when
a page lists what's coming up, when someone opens the event, when a calendar app
fetches the feed. Nothing is copied. That's why editing a repeating event in one
place changes every date at once, and why a series that started years ago still
shows next week's date.

When a listing asks for upcoming events, each repeating one is **expanded** into
one entry per date, so a weekly meet-up appears once a week. Each entry is still
the one post, so it links to the one page.

## Next

For a repeating event the question people ask is "when is the next one?", so
every event knows its **next** date: the first one that hasn't ended yet, or
nothing once the series is over. RSVPs follow the same idea. On a repeating
event, "are you coming?" is about the next date, so a weekly meet-up asks about
this week, and each week's answers are kept apart.

## One feed for everyone, one for each member

Every site publishes its events as a calendar feed that Google Calendar, Apple
Calendar or Outlook can subscribe to. The feed carries the rule, not the dates,
so the calendar app does the repeating and stays in sync as you edit.

A feed is fetched by a calendar app, and a calendar app can't sign in. So the
public feed can only carry what everyone may see: published posts, in collections
anyone can open, outside groups a visitor can't see.

A member can see more: members-only collections they're allowed into, and their
groups' events. For them there's a private feed, whose address carries a
**secret token** that stands in for signing in. The feed is worked out as that
member. Anyone holding the address reads that member's calendar, so it's treated
like a password: a member can reset it, and the old address stops working.

## Where it lives

A post's time is one row in the site's `events` table, beside the post, the same
way a location sits beside it. It is written when the post gets a `when`, changed
when the `when` changes, and removed when the post is deleted or its `when` is
taken away.

Since it belongs to the post, it travels with the post: `friendo push --posts` and
`friendo pull --posts` carry it. See [Calendar](/docs/calendar) for how to write
and list events, and [Tags and filters](/docs/template-filters#filters-friendo-adds)
for the date filters.
