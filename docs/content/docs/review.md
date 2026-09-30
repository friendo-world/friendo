---
title: Review comments and posts
slug: review
section: Guides
topic: Community
weight: 50
description: Choose what waits for a human before it shows, then approve it in the admin's Review page or right on the post.
---

To keep your site tidy, decide what waits for **review**, then approve or reject
it in the admin or on the page itself. A comment or post that waits isn't shown to
anyone else until someone says yes.

## 1. Choose what waits

In admin **Settings**, or as keys under [`[settings]`](/docs/config#settings) in
`friendo.toml`:

| Setting | Where in Settings | Key | Default |
|---|---|---|---|
| **Comments wait for review** | Comments | `comments_need_review` | on |
| **Contributors' posts wait for review** | Members & roles | `posts_need_review` | off |
| **Members can post** | Members & roles | `members_can_post` | off |

With **Members can post** on, plain members can post from a
[`<friendo-form>`](/docs/forms), and their posts *always* wait for review. So do
comments and posts from [visitors](/docs/visitors) (people who aren't signed in),
when you let them comment or post.

The presets in **Settings → Members & roles** set the post rule for you:
**Personal** and **Community** leave it off, **Blog** turns it on.

```toml
[settings]
comments_need_review = true
posts_need_review = true
members_can_post = true
```

A key set in `friendo.toml` wins on every start, and its switch shows read-only
in the admin.

## 2. Review in the admin

Open **Review** in the admin (`/_/review`). It has two parts:

- **Posts to review**: each waiting post with its collection, author and, for an
  event, its date, and the start of its body. A post that came with images shows
  the first as a thumbnail; click it for the full image. A post from a
  [drop box](/docs/visitors#drop-boxes) says *no name kept*. **Edit** opens it, **Publish** makes it live, **Delete**
  removes it.
- **Comments**: tabs for *Pending*, *Approved* and *Rejected*. Each comment has
  **Approve**, **Reject** and **Delete**. A rejected comment can still be
  approved later.

Each part has **Only from visitors**, which narrows it to what people who weren't
signed in wrote. Their names read "Robin (visitor)".

## 3. Or review on the page

When the viewer is the post's author or a site moderator, `<friendo-comments>`
shows waiting comments too, each with **Approve**, **Reject** and **Delete**
buttons. A member sees their own waiting comment marked as such, and can delete
anything they wrote.

## Who reviews what

| Role | Reviews |
|---|---|
| **Moderator** and up | Every comment, and every waiting post |
| **Contributor** | Comments on their own posts |

A moderator can say yes or no to what others wrote, but can't change it; editing
someone else's post takes an editor. See [Roles](/docs/roles).

Requests to join a group aren't in the admin: a group's admins and moderators
approve or decline them in the group's own `<friendo-group>`. See
[Groups](/docs/groups).

Reactions and poll votes are never reviewed. They're one per member.

> **Common mistake:** turning **Comments wait for review** off shows *new*
> comments at once. Comments already waiting stay waiting until someone approves
> them.

**Next:** [Send real email](/docs/email) so members can sign in, or
[Roles](/docs/roles) for everything each role can do.
