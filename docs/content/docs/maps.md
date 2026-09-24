---
title: Maps & locations
slug: maps
group: Concepts
weight: 10
---

Any post can carry a **location** — a pin with a label. One tag shows a post's
pin; the same tag with no post shows every pinned post on one map.

## Geo-tag a post

In a markdown post's front matter, either shape works:

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

The label defaults to the post's title. Remove the field and the pin goes with
the next `friendo build` / `serve` / `push`. (Pins can also be added from the
API — `POST /_/api/locations` — for posts written in the admin.)

## Show one post's map

On the post's page:

```html
<friendo-map target-type="post" target-id="{{ record.id }}"></friendo-map>
<script src="/friendo.js" defer></script>
```

## Show every post on one map

Leave off `target-id` and you get the **aggregate** map: every published post
with a location, each pin linking to its post.

```html
<friendo-map target-type="post"></friendo-map>
```

Pins link to the post's public URL, worked out from your `pages/` routes — so a
post under `pages/blog/[slug].html` links to `/blog/<slug>` without you saying
so. If your links should go somewhere else, set the pattern yourself:

```html
<friendo-map target-type="post" post-url-pattern="/stories/{slug}"></friendo-map>
```

## Styling

The map is [Leaflet](https://leafletjs.com) with OpenStreetMap tiles, loaded
lazily from a CDN when the tag first appears (so a page without a map never
loads it). Its height is a fixed 380px by default — change it from your CSS:

```css
friendo-map::part(map) { height: 520px; }
```
