---
title: Upload images and back up media
slug: images
section: Guides
topic: Content
weight: 70
description: Attach images to posts from the admin, a form or a folder, and keep them safe on disk or in S3-compatible storage.
---

To put an image on a post, upload it from the admin, from a `<friendo-input
type="media">` in a form, or drop it in the post's folder. Every way ends the
same: the file is in your site's `assets/`, served from `/assets/`, the same
locally and deployed.

## From the admin

Open a post in the admin (`/_/`). A field of kind `image` shows **Choose image**.
Pick a file and it previews at once; it uploads when you save the post. The field
then holds the image's URL, `/assets/uploads/…`.

To give every post in a collection an image field, declare it in
[`friendo.toml`](/docs/config#fields):

```toml
[content.blog.fields]
cover = "image"
```

Show it in a template:

```html
{% if post.fields.cover %}<img src="{{ post.fields.cover }}" alt="">{% endif %}
```

## From a form

In a [`<friendo-form>`](/docs/forms), a `media` input picks a file and uploads it
after the post is made:

```html
<friendo-input name="cover" type="media" accept="image/*"></friendo-input>
```

The URL lands in `post.fields.cover`. Uploading needs a contributor or higher,
unless you turn on **Members can add images** (`members_can_upload`) or
**Visitors can add images** (`visitors_can_upload`). Then members or
[visitors](/docs/visitors) can attach images to the post they're submitting, and
the images wait for review with it. Anyone else sees a note instead of the picker.
A [drop box](/docs/visitors#drop-boxes) takes images from anyone.

Until its post is published, an uploaded image is private: it opens only for
moderators, editors and whoever sent it, and `friendo export` leaves it out.

Uploads from the admin and forms go through the [files API](/docs/api) and land
in `assets/uploads/`. Only images are accepted (PNG, JPEG, GIF, WebP, SVG), up to
10 MB each.

## From a folder

A post written as a file can be a folder with an `index.md` inside. Images next
to it become the post's **gallery**, with no upload step:

```
content/blog/my-trip/
  index.md          → blog / my-trip
  01-sunrise.jpg    ┐  post.gallery
  02-harbor.jpg     ┘
```

```html
{% for img in post.gallery %}<img src="{{ img.url }}" alt="">{% endfor %}
```

The images are copied under `assets/galleries/` on each import, in file-name
order. See [A post as a folder](/docs/posts-in-files#a-post-as-a-folder).

## Send them to your deployed site

`friendo push` sends the whole `assets/` folder, images included.
`friendo push --posts` also sends which post each image belongs to, so the
gallery and upload lists match on the other side.

## Back up media

A self-hosted site keeps everything in its folder. Back up two things:

- `data/friendo.db`, the database. It's SQLite in WAL mode: copy it with
  `sqlite3 data/friendo.db ".backup backup.db"`, or stop the server first.
- `assets/`, which holds the uploads and galleries.

To keep uploads and galleries in S3-compatible storage (Cloudflare R2, for one)
instead of on disk, set these on the server before it starts:

```bash
FRIENDO_S3_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
FRIENDO_S3_BUCKET=friendo-media
FRIENDO_S3_ACCESS_KEY=…
FRIENDO_S3_SECRET_KEY=…
FRIENDO_S3_REGION=auto
```

Only `assets/uploads/` and `assets/galleries/` move to the bucket; your own CSS
and images stay on disk. The variables are the same for one site and for a whole
[network](/docs/run-a-network). The full list is in
[Environment variables](/docs/env).

> **Common mistake:** set only some of the four required variables (endpoint,
> bucket, access key, secret key) and media quietly stays on disk. The server's
> startup log says which one is missing: look for a line starting `media:`.

**Next:** [Self-host a site](/docs/self-host-a-site) for the rest of running a
server, or [Write posts in files](/docs/posts-in-files).
