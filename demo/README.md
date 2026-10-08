# Hollowbrook Online — the friendo demo

A small-town community network, circa 2003, built from everything friendo ships:
blogs, classifieds, people with profile pages, groups, a calendar, a map, chat,
an inbox, members-only pages. It is the site you open to **see a feature working,
try a tag, or check a change**, and it is also an example of how a real site is
put together.

```bash
npm run demo          # build everything, serve on :3000, seed the town the first time
npm run demo:reset    # the same, after throwing the database away
node demo/demo.mjs --port 3100     # just serve (bin/friendo must exist)
```

The site is at <http://localhost:3000/>, the admin at <http://localhost:3000/_/>.
On a fresh database `demo.mjs` fills the town in through the public API, exactly
as browsers would, and prints who's who.

## Who lives here

Sign in as anyone: type the email in the sign-in box and the emailed code is
shown right in the form (local dev has no email provider). Every account also
has the password `hollowbrook`, which the seed script uses.

| Who | Email | Role | How they decorated their page |
|---|---|---|---|
| Dana Whitfield | dana@hollowbrook.test | owner | plain |
| Marcus Bell | marcus@hollowbrook.test | moderator | plain |
| Priya Raman | priya@hollowbrook.test | member | thefacebook-blue links |
| Sam Okafor | sam@hollowbrook.test | member | hot pink, Comic Sans, lime links |
| Jordan Ellis | jordan@hollowbrook.test | member | black, Impact, orange links |
| Ruth Kowalski | ruth@hollowbrook.test | member | Times, dark green |

They follow each other, belong to groups (the Town Council is private), have
commented, reacted, voted, chatted and RSVP'd. All of it is in
[`seed/town.json`](seed/town.json); the posts are the Markdown files under
[`seed/posts/`](seed/posts/), each with an `author:` line. Edit either and run
`npm run demo:reset`.

The groups, the events (including the repeating ones) and one photo page bundle
are ordinary [content files](content/) and come in through `friendo serve` like
on any site. They have no author, which is why the people's own posts go through
the API instead.

## The looks

The **Look** menu in the bottom-right corner of every page switches the whole
site's stylesheet and remembers the choice in the browser:

| Look | File | What it is for |
|---|---|---|
| Unstyled | none | No stylesheet at all. Browser defaults plus whatever each `<friendo-*>` tag ships. **Check a new tag here first.** |
| Hollowbrook (2003) | [`assets/themes/board.css`](assets/themes/board.css) | The default. A community board: Verdana, beveled buttons, blue title bars, a hit counter. |
| thefacebook (2005) | [`assets/themes/facebook.css`](assets/themes/facebook.css) | The blue bar, a left column of links, grey hairlines. |
| MySpace (2006) | [`assets/themes/myspace.css`](assets/themes/myspace.css) | Orange headings, blue section bars, and member-decorated profile pages. |

Each file is a complete skin for the same markup: the page chrome first, then a
`::part()` rule for every piece of every tag. That makes each one a worked
example of [styling the tags](https://docs.friendo.world/docs/styling). The
switcher itself is the only thing styled inline, so it stays usable with no
stylesheet.

**Adding a look:** drop `assets/themes/<name>.css` in and add `<name>` to the
`THEMES` list and the `<select>` in [`layouts/base.html`](layouts/base.html).

### Members decorate their own page

The look is always the visitor's pick, but a profile page carries its owner's
decorations on top. `[profiles.fields]` in `friendo.toml` gives every member a
`background` (a colour or an image address), an `accent` colour, a `font`, a
`headline` and a `mood`; they set them with the **Edit profile** button on
their own page (or from the sign-in box). The profile page puts them on
`<body>` as custom properties (`--profile-background`, `--profile-accent`,
`--profile-font`), and each stylesheet reads them under `.profile-page`.
MySpace honours all of them; Hollowbrook the colours and font;
thefacebook only the accent, which is about what thefacebook allowed. There is deliberately no
raw-CSS field: a member's CSS would run for every visitor of their page.

## Where each thing is exercised

| Page | Tags and features on it |
|---|---|
| `/` | `<friendo-reactions>` on each post, `<friendo-chat>` (town square), `collections.*`, `sort_by`, `upcoming`, the hit counter |
| `/blog/`, `/blog/<slug>` | `<friendo-reactions>`, `<friendo-poll>`, `<friendo-comments>`, `<friendo-map>`, `<friendo-follow>`; `post.gallery` (page bundle); server-rendered reactions, poll and comments in the sidebar |
| `/classifieds/`, `/classifieds/<slug>`, `/sell` | A collection with declared fields (`category`, `price`, `contact`, `photo`); `<friendo-form>` with `<friendo-input type="richtext|media|location">` |
| `/profiles/`, `/profiles/<slug>` | `profile.*`, `by_author`, `by_following`, `<friendo-follow>`, `<friendo-profile edit-only>`, per-profile theming |
| `/groups/`, `/groups/<slug>` | `<friendo-groups>`, `<friendo-group>`, group chats, `in_group`, a private group, `<friendo-add-to-calendar subscribe group>` |
| `/events/`, `/events/<slug>` | `when`, `upcoming`, `in_month`, `past`, `<friendo-rsvp names>` (visitors may answer), `<friendo-invite>`, `<friendo-add-to-calendar>`, `google_calendar_url`, an event chat |
| `/calendar` | `<friendo-calendar>` and a `<friendo-form>` with `<friendo-input type="when">` |
| `/map` | The aggregate `<friendo-map>` (every post with a place) |
| `/chat` | `<friendo-chat>` at full height |
| `/write` | `<friendo-form>` for the blog with tags, mood, place, cover and anonymous posting |
| `/inbox` | `{% members only %}` and `<friendo-inbox>` |
| `/council/` | A folder kept for one group by `[access] groups`; `pages/login.html` is the sign-in page it shows |
| every page | `<friendo-signin reload>` in the sidebar, `user.unread` in the masthead |

Not here: `<friendo-account>`, `<friendo-console>` and `<friendo-activate>`,
which only make sense on a network (`npm run dev`).

## Trying a change

1. Put the tag (or the template feature) on the page where it belongs, or on a
   new page under `pages/`.
2. Open it with the look set to **Unstyled** to see exactly what friendo ships.
3. Add its parts to each theme under `assets/themes/`, so the demo keeps showing
   how a site would skin it. The three files share one order and one set of
   section comments, which makes the diff between them readable.
4. If it needs data, add it to `seed/town.json` or a file under `seed/posts/`
   and `npm run demo:reset`.

The database lives in `data/` and is ignored by git, so a clone always starts
fresh. Posts seeded through the API are dated the day the seed ran.
