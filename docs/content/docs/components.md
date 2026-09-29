---
title: Components
slug: components
section: Reference
weight: 60
description: "Every friendo tag: attributes, parts and events."
---

Every `<friendo-*>` tag, with its attributes, the parts you can style with
`::part()`, and the events it fires.

Every site serves the tags at **`/friendo.js`**. Load it once, in your layout, and
use them anywhere:

```html
<script src="/friendo.js" defer></script>
```

| Tag | What it does |
|---|---|
| `<friendo-signin>` | Sign in with an email code; shows who's signed in, their profiles, and a sign-out button |
| `<friendo-comments>` | The approved comments and, for members, a box to write one |
| `<friendo-reactions>` | Emoji reactions with live counts; a click toggles yours |
| `<friendo-poll>` | A poll; members vote once and see the tally |
| `<friendo-chat>` | A realtime message feed |
| `<friendo-map>` | A map of a post's location, or of every post |
| `<friendo-form>` | A form that makes a new post |
| `<friendo-input>` | A rich input inside a `<friendo-form>` |
| `<friendo-calendar>` | A month grid or list of the site's events |
| `<friendo-rsvp>` | Going / Maybe / Can't go on an event |
| `<friendo-invite>` | The organizer's side of an RSVP: invite people |
| `<friendo-add-to-calendar>` | A menu of calendar apps for one event or the whole feed |
| `<friendo-profile>` | A member's profile card, with *Edit profile* on your own |
| `<friendo-follow>` | A follow button with the follower count |
| `<friendo-inbox>` | The member's notifications |
| `<friendo-group>` | One group: join, the members, the admins' levers |
| `<friendo-groups>` | The groups the viewer may see, and a *start a group* form |
| `<friendo-account>` | On a network: your sites, their domains, *Open admin*, a new site |
| `<friendo-console>` | On a network: the operator's levers |
| `<friendo-activate>` | On a network: approve a `friendo login` from the terminal |

Two rules hold for every tag. A tag that works on a post takes `post-id`
(`{{ post.id }}`). And a tag renders into a shadow root with almost no styling, so
your stylesheet reaches it through parts:

```css
friendo-comments::part(submit) { background: rebeccapurple; color: #fff; }
```

Every tag can also show an `error` part when it can't load.

## `<friendo-signin>`

Sign in with an email code; shows who's signed in, a *profiles* switcher and a
sign-out button.

```html
<friendo-signin reload></friendo-signin>
```

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

```html
<friendo-comments post-id="{{ post.id }}"></friendo-comments>
```

| Attribute | What |
|---|---|
| `post-id` | The post |

Parts: `list`, `comment`, `author`, `badge`, `body`, `actions`, `approve`, `reject`,
`delete`, `form`, `input`, `submit`, `status`, `empty`, `signed-out`.

## `<friendo-reactions>`

Emoji reactions with live counts; a click toggles yours. The member's own carry
`aria-pressed="true"`.

```html
<friendo-reactions post-id="{{ post.id }}" emojis="👍,🔥,😂"></friendo-reactions>
```

| Attribute | What |
|---|---|
| `post-id` or `comment-id` | What the reactions are on |
| `emojis` | Comma-separated choices; default `👍,❤️,🎉` |

Parts: `row`, `button`, `emoji`, `count`.

## `<friendo-poll>`

A poll; members vote once and see the tally. See [Polls](/docs/polls).

```html
<friendo-poll poll-slug="next-meetup"></friendo-poll>
```

| Attribute | What |
|---|---|
| `poll-slug` | The slug declared in the post's front matter |
| `poll-id` | The poll's id, instead of `poll-slug` |

Parts: `question`, `option`, `bar`, `result`, `total`.

## `<friendo-chat>`

A realtime message feed. See [Chats](/docs/chats).

```html
<friendo-chat chat-id="general"></friendo-chat>
```

| Attribute | What |
|---|---|
| `chat-id` | The chat's id; naming one makes it |
| `group` | A group's slug: one of its chats, for its members only |

Custom properties: `--chat-mine`, `--chat-theirs`, `--chat-border`,
`--chat-background`, `--chat-height`.

Parts: `chat`, `list`, `message` (and `mine` on your own), `author`, `body`,
`meta`, `sent`, `time`, `delete`, `form`, `input`, `submit`, `status`, `empty`,
`signed-out`, `locked`.

## `<friendo-map>`

A map of a post's [location](/docs/locations), or of every post.

```html
<friendo-map post-url-pattern="/stories/{slug}"></friendo-map>
```

| Attribute | What |
|---|---|
| `post-id` | One post's location; leave it off for every post |
| `post-url-pattern` | Where a marker links, e.g. `/stories/{slug}` |

Parts: `map`, `empty`.

## `<friendo-form>`

A form that makes a new post. See [Forms](/docs/forms).
The one tag that stays in your page's DOM, so its parts are styled by attribute:
`friendo-form [part="error"]`.

```html
<friendo-form collection="stories" redirect="/stories/{slug}">
  <input name="title" required>
  <textarea name="body"></textarea>
  <button type="submit">Share</button>
</friendo-form>
```

| Attribute | What |
|---|---|
| `collection` | Where the post goes (default `posts`) |
| `redirect` | Where to go after, with `{slug}` and `{id}` filled in |
| `status` | The status to ask for (`published`, the default, or `draft`) |

Parts: `status`, `error`. Fires `friendo:submitted` with `detail.post`.

## `<friendo-input>`

A rich input inside a `<friendo-form>`.

```html
<friendo-input name="where" type="location"></friendo-input>
```

| Attribute | What |
|---|---|
| `name` | The field name (`body`, `when`, or any field) |
| `type` | `richtext`, `location`, `media`, `tags` or `when` |
| `placeholder`, `accept` | As on a native input (`accept` defaults to `image/*`) |

Parts: `input`, `fallback`, `toolbar`, `tool`, `editor`, `map`, `coords`, `file`,
`preview`, `note`, `chips`, `chip`, `chip-remove`; for `type="when"`: `when`,
`label`, `checkbox`, `select`.

## `<friendo-calendar>`

A month grid or list of the site's [events](/docs/calendar).

```html
<friendo-calendar collection="events" view="list" limit="10"></friendo-calendar>
```

| Attribute | What |
|---|---|
| `collection` | One collection (default: every event) |
| `view` | `month` (default) or `list` |
| `month` | The month to open on, `YYYY-MM` |
| `limit` | In `list` view, how many dates to show |

Parts: `nav`, `title`, `button`, `view`, `grid`, `weekday`, `day`, `today`,
`outside`, `date`, `event`, `more`, `list`, `group`, `heading`, `time`, `empty`,
`error`.

## `<friendo-rsvp>`

Going / Maybe / Can't go on an [event](/docs/calendar#rsvp).

```html
<friendo-rsvp post-id="{{ post.id }}" names></friendo-rsvp>
```

| Attribute | What |
|---|---|
| `post-id` | The event |
| `date` | An RFC 3339 start, to ask about one date of a repeating event |
| `names` | Also list who answered (shown to the author and moderators) |
| `question`, `going-label`, `maybe-label`, `not-going-label` | Your own wording |

Parts: `question`, `when`, `row`, `button`, `count`, `mine`, `invited`,
`invited-count`, `names`, `name`, `status`, `signed-out`. Fires `friendo:rsvp`
with `postId`, `date` and `answer`.

## `<friendo-invite>`

The organizer's side of an RSVP: invite people by profile name, group or followers.
Paints only for the event's author or a moderator.

```html
<friendo-invite post-id="{{ post.id }}"></friendo-invite>
```

| Attribute | What |
|---|---|
| `post-id` | The event |
| `date` | One date of a repeating event |

Parts: `form`, `input`, `group`, `followers`, `send`, `result`. Fires
`friendo:invite`.

## `<friendo-add-to-calendar>`

A menu of calendar apps (Google Calendar, Apple Calendar, Outlook, download).

```html
<friendo-add-to-calendar post-id="{{ post.id }}"></friendo-add-to-calendar>
```

| Attribute | What |
|---|---|
| `post-id` | Add one event (`date` picks one date of a repeating one) |
| `subscribe` | The whole feed instead; signed in, the member's [private link](/docs/calendar#your-private-calendar) |
| `collection`, `group`, `mine` | Narrow the feed |
| `label` | The button's text |

Parts: `button`, `menu`, `heading`, `item`, `copy`, `reset`, `status`, `error`.

## `<friendo-profile>`

A member's [profile](/docs/profiles) card, with *Edit profile* on your own.

```html
<friendo-profile slug="robin"></friendo-profile>
```

| Attribute | What |
|---|---|
| `slug` | Whose; leave it off for the viewer's own |
| `edit-only` | Just the button, on a page that renders the profile server-side |

Parts: `profile`, `avatar`, `name`, `slug`, `follow`, `bio`, `fields`, `label`,
`value`, `edit`, `editor`, `signed-out`.

## `<friendo-follow>`

A [follow](/docs/profiles) button with the follower count.

```html
<friendo-follow slug="robin"></friendo-follow>
```

| Attribute | What |
|---|---|
| `profile-id` or `slug` | Whom to follow |

Parts: `button`, `count`, `status`. Fires `friendo:follow`.

## `<friendo-inbox>`

The member's [notifications](/docs/profiles), newest first.

```html
<friendo-inbox limit="20"></friendo-inbox>
```

| Attribute | What |
|---|---|
| `limit` | How many to show |

Parts: `list`, `item`, `unread`, `target`, `text`, `time`, `mark`, `mark-all`,
`empty`, `signed-out`. Fires `friendo:notifications`.

## `<friendo-group>`

One [group](/docs/groups): the count, join / ask / leave, the members, and the
admins' and moderators' levers.

```html
<friendo-group post-id="{{ post.id }}"></friendo-group>
```

| Attribute | What |
|---|---|
| `post-id` or `slug` | The group |

Parts: `row`, `count`, `join`, `leave`, `pending`, `status`, `members`, `member`,
`role`, `requests`, `request`, `approve`, `decline`, `add`, `chats`, `chat-row`,
`add-chat`, `settings`, `signed-out`. Fires `friendo:group`.

## `<friendo-groups>`

The groups the viewer may see, and a *start a group* form when they may.

```html
<friendo-groups></friendo-groups>
```

Parts: `list`, `item`, `name`, `count`, `mine`, `empty`, `create`, `input`,
`select`, `submit`, `status`. Fires `friendo:group` after it makes one.

## `<friendo-account>`

A network account's page: your sites, *Open admin* on each, their custom domains,
and a form to make a new site, with how many you have left. Signed out, it shows
an email-code sign-in. It is the network's built-in `/account` (and `/login`)
page; a [home site](/docs/run-a-network#the-home-site) can wrap it in its own
design. It talks to the network's API on the bare domain, not the site's.

```html
<friendo-account></friendo-account>
```

No attributes.

Parts: `signin-wrap`, `intro`, `signin`, `email`, `send`, `status`, `verify`,
`code`, `signin-button`, `back`, `error`, `account`, `signout`, `sites-heading`,
`sites`, `site`, `address`, `open-admin`, `domains-toggle`, `domains-row`,
`domains`, `empty`, `quota`, `new-heading`, `new-site`, `new-subdomain`,
`new-name`, `create`, `new-status`, `domain-list`, `domain`, `verify`,
`remove-domain`, `add-domain`, `domain-input`, `add-domain-button`, `dns`,
`domain-status`, `dns-table`. Fires `friendo:site-created` and `friendo:account`.

## `<friendo-console>`

The operator's page: the home site, sites (create, hold, change owner, delete),
who can join, the site limit, people, invites and custom domains. An account
that isn't an operator sees a note and a link to `/account`. It is the network's
built-in `/network` page. See [Run a network](/docs/run-a-network).

```html
<friendo-console></friendo-console>
```

No attributes.

Parts: the sign-in parts above (`signin-wrap` … `signout`), then `not-operator`,
`summary`, `error`, `home-heading`, `home-site`, `home-select`, `home-save`,
`sites-heading`, `create-site`, `create-subdomain`, `create-name`,
`create-owner`, `create`, `sites`, `site`, `join-heading`, `signups`,
`limit-heading`, `quota`, `quota-input`, `quota-save`, `people-heading`,
`people`, `person`, `invites-heading`, `invite`, `invite-email`, `invite-days`,
`invite-operator`, `invite-send`, `invites`, `domains-heading`, `add-domain`,
`domain-input`, `domain-site`, `domain-add`, `dns`, `domains`. Fires
`friendo:account`.

## `<friendo-activate>`

The browser half of `friendo login`: sign in if needed, then approve the code the
terminal showed. It is the network's built-in `/activate` page, which fills
`code` from `?code=`.

```html
<friendo-activate code="K7QP-2XR9"></friendo-activate>
```

| Attribute | What |
|---|---|
| `code` | The code from the terminal, filled into the form |

Parts: the sign-in parts above, then `approve`, `code`, `approve-button`,
`status`, `done`. Fires `friendo:account`.

## Events

Each event is a `CustomEvent`; what it carries is in `event.detail`. See
[SDK events](/docs/sdk-events) for how to listen for them.

| Event | Fires | `detail` |
|---|---|---|
| `friendo:signin` | on `document`, when a member signs in or out | `user` (or `null`) |
| `friendo:needs-auth` | from `<friendo-follow>`, `<friendo-reactions>`, `<friendo-rsvp>` or `<friendo-poll>` when a visitor tries to use it | |
| `friendo:submitted` | from `<friendo-form>` after it makes a post | `post` |
| `friendo:rsvp` | from `<friendo-rsvp>` after an answer | `postId`, `date`, `answer` |
| `friendo:invite` | from `<friendo-invite>` after inviting | `postId`, `invited` |
| `friendo:follow` | from `<friendo-follow>` | `profileId`, `following` |
| `friendo:profile` | on `document`, when a profile is edited | `profile` |
| `friendo:notifications` | on `document`, from `<friendo-inbox>` after marking notifications read | `unread` |
| `friendo:group` | from `<friendo-group>` / `<friendo-groups>` after a change | `groupId` (and `created` when a group was made) |
| `friendo:account` | on `document`, when a network account signs in or out | `account` (or `null`) |
| `friendo:site-created` | from `<friendo-account>` after it makes a site | `subdomain` |
