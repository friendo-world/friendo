---
title: Style the tags
slug: styling
section: Guides
topic: Community
weight: 70
description: Reach inside the <friendo-*> tags from your own stylesheet with ::part(), custom properties and, for forms, ordinary CSS.
---

To make the `<friendo-*>` tags match your site, style them from your own
stylesheet through the **parts** they expose. Each tag draws into a shadow root,
so your CSS doesn't reach inside by itself; `::part()` is the door.

## 1. Know what you start with

Every tag inherits your page's font and text color and ships only a little
styling of its own: borders, spacing, a highlight on the choice you made. Most
sites need a few rules, not a theme.

## 2. Style a part

Every tag names the pieces you may style. Pick the tag and the part:

```css
friendo-comments::part(submit) { background: rebeccapurple; color: #fff; }
friendo-comments::part(input)  { font-family: inherit; border-radius: 0; }
friendo-poll::part(bar)        { background: #eef; }
friendo-reactions::part(button) { border: 1px solid currentColor; }
friendo-reactions::part(button):hover { background: #f5f5f5; }
```

`:hover` and `:focus` work after `::part()`. The full list of parts for every tag
is in [Components](/docs/components).

Some elements carry two parts at once. Your own messages in a chat are
`message mine`, so `::part(mine)` styles only yours:

```css
friendo-chat::part(mine) { opacity: .9; }
```

## 3. Use the custom properties on chats

`<friendo-chat>` also reads custom properties set on the tag:

```css
friendo-chat {
  --chat-mine: #6d5dfc;      /* your bubbles and the Send button */
  --chat-theirs: #e9e9eb;    /* everyone else's bubbles */
  --chat-border: #d9d9de;
  --chat-background: #fff;
  --chat-height: 60vh;       /* the list, before it scrolls */
}
```

See [Chats](/docs/chats).

## 4. Size the map

`<friendo-map>` is 380px tall by default:

```css
friendo-map::part(map) { height: 520px; }
```

See [Locations](/docs/locations).

## 5. Style forms like the rest of your page

`<friendo-form>` is the one tag that stays in your page's DOM, so your inputs
stay yours: style them with ordinary selectors. Its `status` and `error` lines
are plain elements too, reached by attribute:

```css
friendo-form input, friendo-form select { border: 1px solid #999; }
friendo-form [part="error"] { color: crimson; }
```

The `<friendo-input>` tags inside it have shadow roots of their own; style those
with `::part()`, for example `friendo-input::part(toolbar)`.

## The pressed state

Reaction, poll, RSVP and follow buttons carry `aria-pressed="true"` for the
member's own choice, so screen readers announce it, and the tags highlight it
themselves.

> **Common mistake:** `friendo-reactions::part(button)[aria-pressed="true"]`
> matches nothing. CSS doesn't allow an attribute selector after `::part()`, and
> the browser drops the whole rule. Style `::part(button)` as a whole instead.

**Next:** [Use the tags from your own JavaScript](/docs/sdk-events), or the
[Components](/docs/components) reference for every part.
