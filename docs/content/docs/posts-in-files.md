---
title: Write posts in files
slug: posts-in-files
section: Guides
topic: Content
weight: 10
description: Drop markdown files in content/ and friendo reads them into the site.
---

Write a post as a markdown file and friendo reads it into the database. The
folder is the collection, the file name is the slug, the front matter is the
fields. Nothing to click.

## Add a file

```
content/
  blog/
    hello-world.md      → blog, slug "hello-world"
  events/
    harvest-fair.md     → events, slug "harvest-fair"
```

```markdown
---
title: Hello, world
tags: [intro, welcome]
weight: 1
---

Your **markdown** body. Render it with the `markdown` filter.
```

`title`, `slug`, `status` and `date` are the post's own parts; every other key is a
[field](/docs/posts#fields), read back in a template as `{{ post.fields.tags }}`.
Two keys do more: `when` makes the post an [event](/docs/calendar) and `location`
gives it a [place](/docs/locations). Every key is listed in
[Post front matter](/docs/front-matter).

## See it

`friendo serve` reads `content/` when it starts and again on every edit, so the
post is live as soon as you save. `friendo import` does it on demand; `push` and
`deploy` do it before uploading.

> **Common mistake:** a post that came from a file says so in the admin, and the
> file wins on the next import. Edit it in the file, not in the admin, or the
> change is lost next time `content/` is read.

> **Tip:** a value with a colon in it needs quotes in front matter:
> `title: "Notes: week one"`. Without them the whole front matter fails and the
> post has no title.

## A post as a folder

A post can be a folder with an `index.md` inside. Images sitting next to it become
the post's **gallery**, no upload step:

```
content/blog/my-trip/
  index.md          → blog / my-trip
  01-sunrise.jpg    ┐  post.gallery
  02-harbor.jpg     ┘
```

```html
{% for img in post.gallery %}<img src="{{ img.url }}" alt="">{% endfor %}
```

The images are copied under `assets/galleries/` on each import and travel with
`friendo push --posts`. `.png`, `.jpg`, `.gif`, `.webp` and `.svg` are accepted.
For images uploaded through the admin or a form, see [Upload images](/docs/images).

**Next:** [Write posts in the admin](/docs/posts-in-admin), or
[Posts and collections](/docs/posts) for the model behind this.
