---
title: Template variables
slug: template-variables
section: Reference
weight: 40
description: Every variable a page template can read, and what each one carries.
---

Every page gets `site`, `request`, `collections`, `user`, `visitor` and `calendar`. A post's
page adds `post` (and `event` or `group` when it is one). A profile page adds
`profile`. A `login.html` standing in for a members-only page adds `gate`.

## site

| Variable | What |
|---|---|
| `site.name` | The site's name, from `name` under `[site]` in [friendo.toml](/docs/config) |
| `site.url` | The site's own address as the request saw it (`https://my-site.friendo.world`). In a [static export](/docs/static-export), from `[deploy]`: `https://` plus `domain`, else `target` |

## request

| Variable | What |
|---|---|
| `request.path` | The current URL path (`/blog/hello`) |

## collections

| Variable | What |
|---|---|
| `collections.<name>` | Every **published** post in the collection, newest first. Each carries everything in [post](#post) except the parts marked "own page only" |
| `collections.groups` | The [groups](/docs/groups) the viewer may see, each carrying everything in [group](#group). The rest are already gone |
| `collections.profiles` | Every [profile](/docs/profiles) on the site, by name. Empty for a visitor while profiles are members-only |

A post filed under a group the viewer can't see is left out of every collection.

## user

Empty for a visitor, so `{% if user %}` reads naturally.

| Variable | What |
|---|---|
| `user.id` | The account's id |
| `user.name` | The account's name |
| `user.email` | The account's email |
| `user.role` | `owner`, `admin`, `editor`, `moderator`, `contributor` or `member` |
| `user.profile_id` | The id of the profile their posts and comments are by |
| `user.following` | Profile ids they follow |
| `user.followers` | Profile ids that follow them |
| `user.friends` | Profile ids that follow them and that they follow back |
| `user.groups` | Slugs of the groups they're in: `"board" in user.groups` |
| `user.unread` | How many notifications are waiting in their inbox |

`following`, `followers`, `friends` and `groups` are always lists, empty while
that feature is switched off.

## visitor

Empty for a signed-in member. For everyone else, what a
[visitor](/docs/visitors) may do on this site, and who they are once they've
done something.

| Variable | What |
|---|---|
| `visitor.can.react`, `.vote`, `.rsvp`, `.comment`, `.post`, `.upload` | Whether the site lets visitors do it |
| `visitor.known` | This browser has already reacted, voted, answered, commented or posted |
| `visitor.name` | The name they gave, or empty |

```html
{% if visitor.can.post %}<p>No account needed; your post waits for review.</p>{% endif %}
```

## post

On a post's page (`pages/<collection>/[slug].html`), and for each post in a
collection.

| Variable | What |
|---|---|
| `post.id` | The post's id |
| `post.slug` | Its address within the collection |
| `post.title` | Its title |
| `post.body` | Its body as markdown. Print it with `{{ post.body\|markdown }}` |
| `post.status` | `published` on any page a visitor can reach |
| `post.published_at` | Its published date |
| `post.created`, `post.updated` | When it was made and last changed |
| `post.fields.<name>` | One of its [fields](/docs/posts): `{{ post.fields.mood }}` |
| `post.author_id` | The id of the profile that wrote it |
| `post.author` | That [profile](#profile): `name`, `slug`, `avatar`, `bio`, `url`, `fields`. Empty if the profile is gone |
| `post.when` | Its time, if it's an event. See [post.when](#postwhen). Empty otherwise |
| `post.location` | Its place, if it has one. See [post.location](#postlocation) |
| `post.group` | The [group](#group) it's filed under (`group: board`), or empty |
| `post.collection` | The collection's name. Own page only |
| `post.comments` | Its approved comments, oldest first: `author_name`, `author_avatar`, `body`, `created`. Own page only |
| `post.reactions` | Reaction tallies: `emoji`, `count`. Own page only |
| `post.poll` | Its poll, if its front matter declares one: `question`, `options` (each `text`, `votes`), `total_votes`, `closes_at`. Own page only |
| `post.gallery` | Images from the post's folder, each with a `url`. Own page only |
| `post.rsvps` | An event's RSVP tally for its next date: `going`, `maybe`, `not_going`, `invited`, `date`. Own page only |

A feature switched off in **Settings → Features** renders as if the post had none
of it: `comments` and `reactions` are empty lists, `location` and `rsvps` are empty.

## post.when

The same on `event.when`. Printed on its own, `{{ post.when }}` reads
`Sat Oct 4, 10 am – 4 pm`.

| Variable | What |
|---|---|
| `when.start` | The start, as an RFC 3339 time in the event's zone |
| `when.end` | The end, or empty |
| `when.all_day` | `true` for an all-day event |
| `when.timezone` | The zone name (`America/Los_Angeles`) |
| `when.repeats` | The rule in words (`weekly`, `monthly on the first Tuesday`), or empty for a one-off |
| `when.rule` | The same rule in iCalendar form (`FREQ=WEEKLY`), or empty |
| `when.except` | The skipped dates |
| `when.next` | The next date, with its own `start`, `end`, `all_day`, `timezone`; empty when the series is over |

After an expanding filter (`upcoming`, `past`, `in_month`, `on_day`), each entry's
`when` is one date: `start`, `end`, `all_day`, `timezone`, `repeats`, `rule`, and
`date` set to `true`. See [Tags and filters](/docs/template-filters#filters-friendo-adds).

## post.location

| Variable | What |
|---|---|
| `location.lat` | Latitude |
| `location.lng` | Longitude |
| `location.label` | The place's name, if it has one |

See [Locations](/docs/locations).

## event

On the page of a post with a time, `event` is the same post as `post`. Everything
in [post](#post) works: `{{ event.title }}`, `{{ event.when }}`,
`{{ event.rsvps.going }}`. See [Calendar](/docs/calendar).

## group

On a group's page (`pages/groups/[slug].html`), `group` is the same post as
`post`, plus:

| Variable | What |
|---|---|
| `group.settings.visibility` | `public`, `members` or `private` |
| `group.settings.join` | `open`, `request` or `invite` |
| `group.members` | Its members, as [profiles](#profile), each also with `role` (`admin`, `moderator`, `member`) and `since` |
| `group.admins` | The members whose role is `admin` |
| `group.moderators` | The members whose role is `moderator` |
| `group.member_count` | How many members it has |
| `group.chats` | Its chats, each with `id` and `name` |

Groups in `collections.groups`, and `post.group` on a post, carry the same.
See [Groups](/docs/groups).

## profile

On a profile page (`pages/profiles/[slug].html`), and for each entry in
`collections.profiles`. A profile never includes an email.

| Variable | What |
|---|---|
| `profile.id` | The profile's id |
| `profile.name` | Its name |
| `profile.slug` | Its address: the page is `/profiles/<slug>` |
| `profile.url` | That page's path, `/profiles/<slug>` |
| `profile.avatar` | Its avatar image |
| `profile.bio` | Its short bio |
| `profile.fields.<name>` | A field declared under `[profiles.fields]` in friendo.toml |
| `profile.created` | When it was made |
| `profile.posts` | Its published posts, each like [post](#post) in a collection. Profile page only |
| `profile.comments` | Its approved comments: `body`, `created`, `post_title`, `post_slug`, `post_collection`. Profile page only |
| `profile.followers`, `profile.following` | Profiles, as lists. Profile page only |
| `profile.follower_count`, `profile.following_count` | How many of each. Profile page only |

See [Profiles](/docs/profiles).

## calendar

Links for subscribing to the site's events. Every page has them.

| Variable | What |
|---|---|
| `calendar.ics` | The feed's full address (`https://my-site.friendo.world/calendar.ics`) |
| `calendar.webcal` | The same as `webcal://`, for Apple Calendar and Outlook |
| `calendar.google` | Google Calendar's add-a-calendar link |
| `calendar.json` | The events as JSON (`/calendar.json`) |

With no known address (a static export with no `[deploy]`), `calendar.ics` and
`calendar.json` are relative paths and `webcal` and `google` are empty.

## gate

Only on your `pages/login.html` when it stands in for a page the viewer may not see.
See [Members-only pages](/docs/members-only).

| Variable | What |
|---|---|
| `gate.reason` | `signin` (nobody is signed in), `role` (signed in, role too low) or `condition` (the tag's `if` was false) |
| `gate.required` | The lowest role the page asked for (`member`, `editor`, …) |
| `gate.path` | The path the viewer asked for |
