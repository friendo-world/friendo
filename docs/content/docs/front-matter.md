---
title: Post front matter
slug: front-matter
section: Reference
weight: 30
description: "Every key a content file's front matter can carry, and where each one ends up on the post."
---

**Front matter** is the YAML block between two `---` lines at the top of a file in
`content/`. The file's folder is the collection and its name is the slug. See
[Write posts in files](/docs/posts-in-files).

```markdown
---
title: Harvest Fair
when: 2026-10-04 10:00 to 16:00
location: 47.6062, -122.3321
mood: festive
---
Bring a dish.
```

## Keys

| Key | What | Default | In templates |
|---|---|---|---|
| `title` | The post's title | the slug | `post.title` |
| `slug` | The post's slug, overriding the file name | the file name (or folder name for `index.md`) | `post.slug` |
| `status` | `draft`, `pending` or `published` | `published` | `post.status` |
| `date` | The published date. Not an event's time | empty | `post.published_at` |
| `when` | Makes the post an [event](/docs/calendar). See [when](#when) | none | `post.when` |
| `location` | Gives the post a [place](/docs/locations). See [location](#location) | none | `post.location` |
| `poll` | Declares a poll. See [poll](#poll) | none | `post.poll`, `post.fields.poll` |
| `group` | Files the post under a [group](/docs/groups), by the group's slug | none | `post.group`, `post.fields.group` |
| `visibility` | On a post in `groups`: `public`, `members` or `private` | `public` | `group.settings.visibility` |
| `join` | On a post in `groups`: `open`, `request` or `invite` | `open` | `group.settings.join` |
| anything else | A **field** | | `post.fields.<name>` |

`title`, `slug`, `status`, `date`, `when` and `location` are taken off the post's
fields. Every other key, including `poll`, `group`, `visibility` and `join`, stays in
`post.fields`.

## when

A string is the start, or a start and an end:

| `when:` | Meaning |
|---|---|
| `2026-10-04` | all day |
| `2026-10-04 19:00` | starts at 7 pm, no end |
| `2026-10-04 19:00 to 21:00` | 7 to 9 pm |
| `2026-10-04 22:00 to 02:00` | crosses midnight |
| `2026-10-04 to 2026-10-06` | three days, all day |
| `mondays 19:00 to 20:30` | every Monday from now on |

A map spells out the parts:

| Key | Value |
|---|---|
| `start` | Required. Any of the shorthands above |
| `end` | A time (`20:30`) or a full date and time |
| `all_day` | `true` or `false` |
| `timezone` | An IANA zone name (`Europe/Paris`) for this event only. Default: the site's `timezone` |
| `repeats` | `daily`, `weekly`, `monthly`, `yearly`, `weekdays`, `every 2 weeks`, or a map (below) |
| `except` | A list of dates to skip: `[2026-11-25, 2026-12-23]` |
| `rrule` | An iCalendar rule, used verbatim: `"FREQ=WEEKLY;BYDAY=TU,TH"`. Wins over `repeats` |

`repeats` as a map:

| Key | Value |
|---|---|
| `every` | Required. `day`, `week`, `month`, `year`, or `2 weeks` |
| `on` | Weekly: `[tue, thu]`. Monthly: `first tuesday`, `last friday`, `15` |
| `until` | A date, through the end of that day |
| `count` | How many dates |

```yaml
when:
  start: 2026-10-06 19:00
  end: 20:30
  timezone: Europe/Paris
  repeats:
    every: month
    on: first tuesday
    until: 2027-06-30
  except: [2026-12-01]
```

A `when` that can't be read is a warning on import; the post is still saved, with
no event.

## location

Either shape:

```yaml
location: 48.8584, 2.2945
```

```yaml
location:
  lat: 40.6892
  lng: -74.0445
  label: Statue of Liberty
```

| Key | Value |
|---|---|
| `lat` | −90 to 90 |
| `lng` | −180 to 180 |
| `label` | Optional. Default: the post's title |

Coordinates that don't parse or are out of range give the post no location. Remove
the key and the location goes on the next import.

## poll

```yaml
poll:
  slug: cats-or-dogs
  question: Cats or dogs?
  options: [Cats, Dogs]
  closes_at: 2026-12-31T00:00:00Z
```

| Key | Value |
|---|---|
| `slug` | Required. Names the poll: `<friendo-poll poll-slug="cats-or-dogs">` |
| `question` | Required |
| `options` | Required. Two or more |
| `closes_at` | Optional. When voting stops |

The poll is made the first time it's viewed. Changing the question or options keeps
the votes; a new `slug` starts a new poll. See [Polls](/docs/polls).

## A post as a folder

A folder with an `index.md` inside is one post. The folder name is the slug:

```
content/blog/my-trip/
  index.md          → blog / my-trip
  01-sunrise.jpg    ┐  post.gallery
  02-harbor.jpg     ┘
```

| | |
|---|---|
| Gallery files | `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.svg` beside `index.md` |
| Order | By file name |
| Copied to | `assets/galleries/<collection>/<slug>/`, rebuilt on each import |
| In templates | `post.gallery`, each with a `url` |

## Quoting

YAML reads `: ` (a colon and a space) as the start of a new key. A value that
contains one must be quoted:

```diff
-title: Rome: a week on foot
+title: "Rome: a week on foot"
```

If the front matter isn't valid YAML, friendo drops **all** of it, with no warning:
the post gets its file name as title, `published` as status and no fields. The body
still imports.

## Reserved

| Name | Why |
|---|---|
| `content/profiles/` | `profiles` is where member [profiles](/docs/profiles) render; files there are skipped with a warning |
| top-level `content/*.md` | Go to the `pages` collection |
