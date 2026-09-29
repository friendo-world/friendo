---
title: Add sign-in and comments
slug: add-a-community
section: Tutorials
weight: 30
description: Let visitors sign in, comment and react on your blog posts, and approve their comments from the admin. About fifteen minutes.
---

In this tutorial you turn the blog on a fresh site into a place people can talk
back. You add comments and reactions to every post, sign in as a **member**,
write a comment, approve it as the owner, and render the approved comments
without JavaScript. You need a site from [Your first site](/docs/first-site)
running with `friendo serve`.

## 1. Start from the scaffold

If you don't have a site yet:

```bash
friendo init my-site
cd my-site
friendo serve
```

Open `http://localhost:3000/blog/hello-world`. You see the post's title, its
body and a *Back* link. The top of the page already has a sign-in box: the
scaffold's `layouts/base.html` loads the tags with
`<script src="/friendo.js" defer>` and puts `<friendo-signin reload>` in the nav.
Every page that extends the layout can use the tags.

## 2. Add comments and reactions to the post page

Open `pages/blog/[slug].html` and add two tags under the body:

```diff
 {% extends "layouts/base.html" %}
 {% block content %}
 <article>
     <h1>{{ post.title }}</h1>
     <div>{{ post.body|markdown }}</div>
+
+    <friendo-reactions post-id="{{ post.id }}"></friendo-reactions>
+    <friendo-comments post-id="{{ post.id }}"></friendo-comments>
+
     <p><a href="/">&larr; Back</a></p>
 </article>
 {% endblock %}
```

`{{ post.id }}` is the post's stable id; every tag that works on a post takes it
as `post-id`.

Save. The browser reloads and shows three reaction buttons (👍 ❤️ 🎉, each with
a count), *No comments yet.* and *Sign in to join the conversation.* A visitor
can read, but has to sign in to join in.

## 3. Sign in as a member

In the sign-in box in the nav, type any email, say `sam@example.com`, and press
**Email me a code**.

No email is sent: on your own machine there's no email provider, so the form
shows the code instead (*Dev code: 123456*) and fills it in for you. Press
**Verify**.

The page reloads and the nav reads *Posting as sam · profiles · Sign out*. You're
now a member of your own site. Your name starts as the part of the email before
the `@`.

> **Note:** On a real server the code is emailed and never shown. See
> [Email](/docs/email) for wiring up a provider.

## 4. Write a comment

Under the post, type *Lovely first post.* in the box and press **Post comment**.

Your comment appears in the list, dashed, marked **pending**, with a **Delete**
button. It is waiting for **review**: new comments stay hidden from everyone else
until a moderator approves them. Only you, the author of the comment, see it now.

Click a reaction too. Its count goes to 1 and the button stays pressed. Reactions
are never reviewed; they're one per member and a second click takes yours back.

## 5. Approve it in the admin

Open a **private window** and go to `http://localhost:3000/_/`. On your own
machine the admin opens with no sign-in and you're the owner.

> **Note:** Use a private window because your normal window is signed in as
> `sam`, and a signed-in session always wins: the admin would open as sam, a
> plain member, with no review page. Signing out in the nav works too.

Click **review** in the admin's top bar. The **Review** page lists what's
waiting. Under **Comments**, on the **Pending** tab, you see sam's comment. Press
**Approve**.

Back in your first window, reload the post. The comment is there with no
**pending** mark, and a signed-out visitor sees it too.

> **Tip:** Don't want comments to wait? Turn off **Comments wait for review** in
> the admin under **Settings**, and they show at once. See [Review](/docs/review).

## 6. Render the comments without JavaScript

The tag loads its comments in the browser. For readers without JavaScript, and
for search engines, the same approved comments are on `post.comments`, so the
template can print them itself:

```diff
     <friendo-reactions post-id="{{ post.id }}"></friendo-reactions>
+
+    <h2>Comments</h2>
+    <ol>
+    {% for c in post.comments %}
+        <li><b>{{ c.author_name }}</b>: {{ c.body }}</li>
+    {% empty %}
+        <li>No comments yet.</li>
+    {% endfor %}
+    </ol>
+
     <friendo-comments post-id="{{ post.id }}"></friendo-comments>
```

View the page source (not the inspector): the approved comment is in the HTML,
as `<li><b>sam</b>: Lovely first post.</li>`. Waiting comments never appear
here; only approved ones do.

Now the page shows the comments twice: once from the server, once from the tag.
Keep both while you're building, or keep the list for reading and give the tag
its own spot for writing. The two ways of saying the same thing are explained in
[Two spellings](/docs/two-spellings).

## 7. Style a part

Each tag draws into its own shadow root, so your stylesheet can't reach inside
by accident. It reaches the pieces a tag exposes as **parts**. Add a line to
`assets/style.css`:

```css
friendo-comments::part(submit) { background: #2563eb; color: #fff; border: 0; padding: 0.4rem 0.8rem; }
```

Save and reload. The **Post comment** button turns blue. Every tag's parts are
listed in [Components](/docs/components); [Styling](/docs/styling) has more.

> **What you built.** A blog where visitors sign in with an email code, react
> and comment, and every comment waits for your review before anyone else sees
> it. The approved comments are rendered on the server, and the tags are styled
> from your own stylesheet. No JavaScript of your own.

**Next:** [Build an events site](/docs/events-site)
