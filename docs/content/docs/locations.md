---
title: Put posts on a map
slug: locations
section: Guides
topic: Content
weight: 50
description: Give a post a place and draw it on a map.
---

Any post can have a **location**: a place with a label. One tag draws a post's
location on a map; the same tag with no post draws every post on one map.

## Give a post a location

In a content file's front matter, either shape works:

```yaml
---
title: A day at the harbour
location: 48.8584, 2.2945
---
```

```yaml
---
title: Meet-up
location:
  lat: 40.6892
  lng: -74.0445
  label: Statue of Liberty
---
```

The label defaults to the post's title. Remove the key and the location goes with
the next import. In the admin, every post has a **Location** section; a
[`<friendo-form>`](/docs/forms) with a
`<friendo-input type="location">` gives a post one from a page.

Templates see it as `post.location`: `lat`, `lng`, `label`. An [event's](/docs/calendar)
location goes out in the calendar feed as the place.

## Show one post's map

```html
<friendo-map post-id="{{ post.id }}"></friendo-map>
<script src="/friendo.js" defer></script>
```

## Show every post on one map

Leave off `post-id` and every published post with a location is on the map, each
marker linking to its post:

```html
<friendo-map></friendo-map>
```

Markers link to the post's URL, worked out from your `pages/` routes: a post under
`pages/blog/[slug].html` links to `/blog/<slug>`. If your links should go somewhere
else, say so:

```html
<friendo-map post-url-pattern="/stories/{slug}"></friendo-map>
```

## Styling

The map is [Leaflet](https://leafletjs.com) with OpenStreetMap tiles, loaded from
a CDN the first time a map appears on a page (a page without one never loads it).
Its height is 380px by default; change it from your CSS:

```css
friendo-map::part(map) { height: 520px; }
```

Locations are a [feature](/docs/config#settings): switch them off and the map
draws nothing.
