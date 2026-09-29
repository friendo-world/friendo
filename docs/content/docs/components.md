---
title: Components
slug: components
section: Reference
weight: 10
---

Every `<friendo-*>` tag, with its attributes, the parts you can style with
`::part()`, and the events it fires. Load `/friendo.js` once and they all work.
The [Community features](/docs/community) page shows them in use.

Two rules hold for every tag. A tag that works on a post takes `post-id`
(`{{ post.id }}`). And a tag renders into a shadow root with almost no styling, so
your stylesheet reaches it through parts:

```css
friendo-comments::part(submit) { background: rebeccapurple; color: #fff; }
```

## `<friendo-signin>`

Sign in with an email code; shows who's signed in, a *profiles* switcher and a
sign-out button.

| Attribute | What |
|---|---|
| `reload` | Reload the page after signing in or out (for pages that render `{{ user }}`) |

Parts: `form`, `email`, `code`, `button`, `status`, `signed-in`, `name`, `badge`,
`signout`, `profiles-toggle`, `profiles`, `profile`, `edit-profile`,
`profile-editor`, `new-profile`, `new-name`, `add`, `label`, `field`, `input`,
`save`, `cancel`.

## `<friendo-comments>`

The approved comments and, for members, a box to write one. The post's author
and site moderators see waiting comments with approve / reject / delete.

| Attribute | What |
|---|---|
| `post-id` | The post |

Parts: `list`, `comment`, `author`, `badge`, `body`, `actions`, `approve`, `reject`,
`delete`, `form`, `input`, `submit`, `status`, `empty`, `signed-out`.

## `<friendo-reactions>`

Emoji reactions with live counts; a click toggles yours. The member's own carry
`aria-pressed="true"`.

| Attribute | What |
|---|---|
| `post-id` or `comment-id` | What the reactions are on |
| `emojis` | Comma-separated choices; default `👍,❤️,🎉` |

Parts: `row`, `button`, `emoji`, `count`.

## `<friendo-poll>`

A poll; members vote once and see the tally.

| Attribute | What |
|---|---|
| `poll-slug` | The slug declared in the post's front matter (or `poll-id`) |

Parts: `question`, `option`, `bar`, `result`, `total`.

## `<friendo-chat>`

A realtime message feed. See [Chats](/docs/chats).

| Attribute | What |
|---|---|
| `chat-id` | The chat's id; naming one makes it |
| `group` | A group's slug: one of its chats, for its members only |

Custom properties: `--chat-mine`, `--chat-theirs`, `--chat-border`,
`--chat-background`, `--chat-height`.

Parts: `chat`, `list`, `message` (and `mine` on your own), `author`, `body`,
`meta`, `sent`, `time`, `delete`, `form`, `input`, `submit`, `status`, `empty`,
`signed-out`, `locked`, `error`.

## `<friendo-map>`

A map of a post's [location](/docs/locations), or of every post.

| Attribute | What |
|---|---|
| `post-id` | One post's location; leave it off for every post |
| `post-url-pattern` | Where a marker links, e.g. `/stories/{slug}` |

Parts: `map`, `empty`.

## `<friendo-form>`

A form that makes a new post. See [Posting from a page](/docs/community#posting-from-a-page).
The one tag that stays in your page's DOM, so its parts are styled by attribute:
`friendo-form [part="error"]`.

| Attribute | What |
|---|---|
| `collection` | Where the post goes |
| `redirect` | Where to go after, with `{slug}` and `{id}` filled in |
| `status` | The status to ask for (`published` or `draft`) |

Parts: `status`, `error`. Fires `friendo:submitted` with `detail.post`.

## `<friendo-input>`

A rich input inside a `<friendo-form>`.

| Attribute | What |
|---|---|
| `name` | The field name (`body`, `when`, or any field) |
| `type` | `richtext`, `location`, `media`, `tags` or `when` |
| `placeholder`, `accept` | As on a native input |

Parts: `input`, `fallback`, `toolbar`, `tool`, `editor`, `map`, `coords`, `file`,
`preview`, `note`, `chips`, `chip`, `chip-remove`; for `type="when"`: `when`,
`label`, `checkbox`, `select`.

## `<friendo-calendar>`

A month grid or list of the site's [events](/docs/calendar).

| Attribute | What |
|---|---|
| `collection` | One collection (default: every event) |
| `view` | `month` (default) or `list` |
| `month` | The month to open on, `YYYY-MM` |
| `limit` | In `list` view, how many dates to show |
| `mine` | Signed in: only the events the member answered or was invited to |

Parts: `nav`, `title`, `button`, `view`, `grid`, `weekday`, `day`, `today`,
`outside`, `date`, `event`, `more`, `list`, `group`, `heading`, `time`, `empty`,
`error`.

## `<friendo-rsvp>`

Going / Maybe / Can't go on an [event](/docs/calendar#rsvp).

| Attribute | What |
|---|---|
| `post-id` | The event |
| `date` | An RFC 3339 start, to ask about one date of a repeating event |
| `names` | Also list who answered (shown to the author and moderators) |
| `question`, `going-label`, `maybe-label`, `not-going-label` | Your own wording |

Parts: `question`, `when`, `row`, `button`, `count`, `mine`, `invited`,
`invited-count`, `names`, `name`, `status`, `signed-out`, `error`. Fires
`friendo:rsvp` with `postId`, `date` and `answer`.

## `<friendo-invite>`

The organizer's side of an RSVP: invite people by profile name, group or followers.
Paints only for the event's author or a moderator.

| Attribute | What |
|---|---|
| `post-id` | The event |
| `date` | One date of a repeating event |

Parts: `form`, `input`, `group`, `followers`, `send`, `result`.

## `<friendo-add-to-calendar>`

A menu of calendar apps (Google Calendar, Apple Calendar, Outlook, download).

| Attribute | What |
|---|---|
| `post-id` | Add one event (`date` picks one date of a repeating one) |
| `subscribe` | The whole feed instead; signed in, the member's [private link](/docs/calendar#your-private-calendar) |
| `collection`, `group`, `mine` | Narrow the feed |
| `label` | The button's text |

Parts: `button`, `menu`, `heading`, `item`, `copy`, `reset`, `status`, `error`.

## `<friendo-profile>`

A member's [profile](/docs/signing-in#profiles) card, with *Edit profile* on your own.

| Attribute | What |
|---|---|
| `slug` | Whose; leave it off for the viewer's own |
| `edit-only` | Just the button, on a page that renders the profile server-side |

Parts: `profile`, `avatar`, `name`, `slug`, `follow`, `bio`, `fields`, `label`,
`value`, `edit`, `editor`, `signed-out`.

## `<friendo-follow>`

A [follow](/docs/community#following) button with the follower count.

| Attribute | What |
|---|---|
| `profile-id` or `slug` | Whom to follow |

Parts: `button`, `count`, `status`. Fires `friendo:follow`.

## `<friendo-inbox>`

The member's [notifications](/docs/community#notifications), newest first.

| Attribute | What |
|---|---|
| `limit` | How many to show |

Parts: `list`, `item`, `unread`, `target`, `text`, `time`, `mark`, `mark-all`,
`empty`, `signed-out`.

## `<friendo-group>`

One [group](/docs/groups): the count, join / ask / leave, the members, and the
admins' and moderators' levers.

| Attribute | What |
|---|---|
| `post-id` or `slug` | The group |

Parts: `row`, `count`, `join`, `leave`, `pending`, `status`, `members`, `member`,
`role`, `requests`, `request`, `approve`, `decline`, `add`, `chats`, `chat-row`,
`add-chat`, `settings`, `signed-out`. Fires `friendo:group`.

## `<friendo-groups>`

The groups the viewer may see, and a *start a group* form when they may.

Parts: `list`, `item`, `name`, `count`, `mine`, `empty`, `create`, `input`,
`select`, `submit`, `status`.

## Network tags

`<friendo-account>`, `<friendo-console>` and `<friendo-activate>` are the tags
behind a network's [built-in pages](/docs/run-a-network#the-home-site). A home
site can wrap them in its own design.

## Events

| Event | Fires | `detail` |
|---|---|---|
| `friendo:signin` | on `document`, when a member signs in or out | `user` (or `null`) |
| `friendo:needs-auth` | from a tag a visitor tried to use | |
| `friendo:submitted` | from `<friendo-form>` after it makes a post | `post` |
| `friendo:rsvp` | from `<friendo-rsvp>` after an answer | `postId`, `date`, `answer` |
| `friendo:follow` | from `<friendo-follow>` | `profileId`, `following` |
| `friendo:profile` | when a profile is edited | `profile` |
| `friendo:group` | from `<friendo-group>` / `<friendo-groups>` after a change | `groupId` |
| `friendo:invite` | from `<friendo-invite>` after inviting | |
