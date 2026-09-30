package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Init creates a new Friendo site directory with the canonical structure.
func Init(name string) error {
	if name == "" {
		name = "my-site"
	}

	// Check if directory already exists.
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	dirs := []string{
		filepath.Join(name, "pages"),
		filepath.Join(name, "pages", "blog"),
		filepath.Join(name, "pages", "events"),
		filepath.Join(name, "pages", "profiles"),
		filepath.Join(name, "pages", "groups"),
		filepath.Join(name, "content", "groups"),
		filepath.Join(name, "layouts"),
		filepath.Join(name, "assets"),
		filepath.Join(name, "content", "blog"),
		filepath.Join(name, "content", "events"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(name, "friendo.toml"):                         friendoToml(name),
		filepath.Join(name, "layouts", "base.html"):                 baseHTML,
		filepath.Join(name, "pages", "index.html"):                  indexHTML,
		filepath.Join(name, "pages", "404.html"):                    notFoundHTML,
		filepath.Join(name, "pages", "blog", "[slug].html"):         blogPostHTML,
		filepath.Join(name, "assets", "style.css"):                  styleCSS,
		filepath.Join(name, "content", "blog", "hello-world.md"):    helloPostMD,
		filepath.Join(name, "pages", "events", "index.html"):        eventsIndexHTML,
		filepath.Join(name, "pages", "events", "[slug].html"):       eventPageHTML,
		filepath.Join(name, "content", "events", "first-meetup.md"): firstMeetupMD(),
		filepath.Join(name, "pages", "profiles", "[slug].html"):     profilePageHTML,
		filepath.Join(name, "pages", "inbox.html"):                  inboxHTML,
		filepath.Join(name, "pages", "groups", "index.html"):        groupsIndexHTML,
		filepath.Join(name, "pages", "groups", "[slug].html"):       groupPageHTML,
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}

	return nil
}

func friendoToml(name string) string {
	return fmt.Sprintf(`[site]
name = "%s"
timezone = "%s"   # the zone an event's time is read in (an IANA name)

[content]
collections = ["blog", "events", "groups", "pages"]

# Optional: the fields a collection's posts carry, so the admin lays out its table and
# form from them (and they travel with the folder). A bare string is the kind: text,
# paragraph, number, checkbox, tags, image or json. A table adds choices, required
# and a hint. Records can still carry other fields; these are just the known ones.
# [content.blog.fields]
# tags  = "tags"
# cover = "image"
# mood  = { kind = "text", choices = ["calm", "wild"], required = true, hint = "How it feels" }

# Optional: make this file the source of truth for these site settings. Uncomment a
# key and it is applied on startup and shown read-only in the admin UI (Settings).
# [settings]
# open_signups = true                # anyone can sign up (off: only people an admin adds)
# signups_are_contributors = false   # a new member starts as a contributor (can write their own posts)
# members_can_post = false           # members can post from a <friendo-form>; their posts wait for review
# posts_need_review = false          # contributors' posts wait for review before they go live
# comments_need_review = true        # comments wait for review before they show
# password_login = false             # also allow signing in with a password (everyone can always use an emailed code)
# profile_visibility = "members"     # who sees profiles (/profiles/<slug>): "members" or "public"
# members_can_start_groups = false   # any member can start a group (and is its admin); off = contributors and up
# comments = true                    # feature switches (Settings → Features): comments, reactions, polls, rsvp, locations, chats, follows, groups
# default_collections = ["blog", "pages"]   # built-in collections listed while [content] collections is unset

# Optional: the fields a member's profile carries beyond name, avatar and bio.
# Same grammar as a collection's fields; <friendo-profile> and the profile
# editor lay themselves out from it. Values live in {{ profile.fields }} on
# pages/profiles/[slug].html.
# [profiles.fields]
# pronouns = "text"
# website  = { kind = "text", hint = "https://…" }

# Optional: members-only paths. A page can also gate itself with {%% members only %%}
# (or editors only, admins only …) at the top of its template. Restart to apply.
# [access]
# members_only = ["/members/*"]   # signed-in people only: this path and everything under it
# editors_only = ["/newsroom/*"]  # editors and up
# groups = { "/board/*" = "board" }   # one group's members only (a page can also say {%% members only if "board" in user.groups %%})

[deploy]
# domain = "mysite.com"
`, name, MachineTimezone())
}

// MachineTimezone is the IANA name of this machine's zone, for a fresh
// friendo.toml: $TZ if set, else what /etc/localtime points at, else UTC.
func MachineTimezone() string {
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			name := target[i+len("zoneinfo/"):]
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	if name := time.Local.String(); name != "Local" && name != "" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return "UTC"
}

const baseHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ site.name }}</title>
    <link rel="stylesheet" href="/assets/style.css">
</head>
<body>
    <nav>
        <a href="/">{{ site.name }}</a>
        <a href="/events/">Events</a>
        <a href="/groups/">Groups</a>
        {% if user %}<a href="/inbox">Inbox{% if user.unread %} ({{ user.unread }}){% endif %}</a>{% endif %}
        <friendo-signin reload></friendo-signin>
    </nav>
    <main>
        {% block content %}{% endblock %}
    </main>
    <script src="/friendo.js" defer></script>
</body>
</html>
`

const indexHTML = `{% extends "layouts/base.html" %}
{% block content %}
<h1>Welcome to {{ site.name }}</h1>

{% for post in collections.blog %}
<article>
    <h2><a href="/blog/{{ post.slug }}">{{ post.title }}</a></h2>
    <div>{{ post.body|markdown }}</div>
</article>
{% empty %}
<p>No blog posts yet. Create one in the <a href="/_/">admin UI</a>.</p>
{% endfor %}
{% endblock %}
`

const notFoundHTML = `{% extends "layouts/base.html" %}
{% block content %}
<h1>404 — Page not found</h1>
<p>There's nothing at <code>{{ request.path }}</code>.</p>
<p><a href="/">Go home</a></p>
{% endblock %}
`

const blogPostHTML = `{% extends "layouts/base.html" %}
{% block content %}
<article>
    <h1>{{ post.title }}</h1>
    <div>{{ post.body|markdown }}</div>
    <p><a href="/">&larr; Back</a></p>
</article>
{% endblock %}
`

// helloPostMD is a starter content file. Running ` + "`friendo import`" + ` (or
// ` + "`friendo serve`" + `) compiles content/blog/*.md into the blog collection.
const helloPostMD = `---
title: Hello, world
slug: hello-world
---

Welcome to your new **Friendo** site.

This post lives in ` + "`content/blog/hello-world.md`" + ` — a plain markdown file.
Edit it, add more files under ` + "`content/`" + `, and run ` + "`friendo serve`" + ` to see
changes live. The folder under ` + "`content/`" + ` is the collection; the front matter
above sets the title and slug.
`

const eventsIndexHTML = `{% extends "layouts/base.html" %}
{% block content %}
<h1>Events</h1>

{# 'upcoming' expands repeating events into one entry per date, soonest first. #}
{% for e in collections.events|upcoming %}
<article>
    <h2><a href="/events/{{ e.slug }}">{{ e.title }}</a></h2>
    <p>{{ e.when|when }}{% if e.when.repeats %} &middot; {{ e.when.repeats }}{% endif %}</p>
</article>
{% empty %}
<p>Nothing coming up. Add a file to <code>content/events/</code> with a <code>when:</code> line.</p>
{% endfor %}

<p>Subscribe: <a href="{{ calendar.google }}">Google Calendar</a> &middot;
<a href="{{ calendar.webcal }}">Apple Calendar / Outlook</a> &middot;
<a href="{{ calendar.ics }}">feed address</a></p>
{% endblock %}
`

const eventPageHTML = `{% extends "layouts/base.html" %}
{% block content %}
<article>
    <h1>{{ event.title }}</h1>
    <p>{{ event.when|when }}{% if event.when.repeats %} &middot; {{ event.when.repeats }}{% endif %}</p>
    {% if event.when.repeats and event.when.next %}<p>Next: {{ event.when.next|when }}</p>{% endif %}
    <div>{{ event.body|markdown }}</div>
    <p><a href="{{ event|google_calendar_url }}">Add to Google Calendar</a> &middot;
    <a href="/calendar.ics?post={{ event.id }}">Download .ics</a> &middot; <a href="/events/">All events</a></p>
</article>
{% endblock %}
`

// profilePageHTML is a member's page at /profiles/<slug>: the profile's name,
// avatar and bio rendered server-side, an Edit button for its owner, and what
// they've written. Who may see it follows [settings] profile_visibility.
const profilePageHTML = `{% extends "layouts/base.html" %}
{% block content %}
<article class="profile">
  {% if profile.avatar %}<img class="avatar" src="{{ profile.avatar }}" alt="" width="96" height="96">{% endif %}
  <h1>{{ profile.name }}</h1>
  {% if profile.bio %}<p class="bio">{{ profile.bio }}</p>{% endif %}
  {# The tag adds an "Edit profile" button when this is the viewer's own page. #}
  <friendo-profile slug="{{ profile.slug }}" edit-only></friendo-profile>
  <p class="follow">
    <friendo-follow profile-id="{{ profile.id }}"></friendo-follow>
    <small>{{ profile.follower_count }} followers · {{ profile.following_count }} following</small>
  </p>

  <h2>Posts</h2>
  <ul>
  {% for p in profile.posts %}
    <li><a href="/{{ p.collection }}/{{ p.slug }}">{{ p.title }}</a> <small>{{ p.created|date:"Jan 2, 2006" }}</small></li>
  {% empty %}
    <li>Nothing yet.</li>
  {% endfor %}
  </ul>
</article>
{% endblock %}
`

// groupsIndexHTML lists the groups the viewer may see and lets them start one.
const groupsIndexHTML = `{% extends "layouts/base.html" %}
{% block content %}
<h1>Groups</h1>
<ul>
{% for g in collections.groups %}
  <li><a href="/groups/{{ g.slug }}">{{ g.title }}</a> <small>{{ g.member_count }} members · {{ g.settings.visibility }}</small></li>
{% empty %}
  <li>No groups yet.</li>
{% endfor %}
</ul>
{# The live side: the same list with your standing, and a "start a group" form when you may. #}
<friendo-groups></friendo-groups>
{% endblock %}
`

// groupPageHTML is one group's page: what it's about, who's in it, the posts
// and events filed under it (group: <slug> in their front matter), and a chat.
const groupPageHTML = `{% extends "layouts/base.html" %}
{% block content %}
<article class="group">
  <h1>{{ group.title }}</h1>
  <div>{{ group.body|markdown }}</div>
  <p><small>{{ group.member_count }} members · {{ group.settings.visibility }} · join: {{ group.settings.join }}</small></p>

  {# Join / leave, the member list, and the moderators' levers. #}
  <friendo-group post-id="{{ group.id }}"></friendo-group>

  <h2>Posts</h2>
  <ul>
  {% for p in collections.blog|in_group:group.slug %}
    <li><a href="/blog/{{ p.slug }}">{{ p.title }}</a> by {{ p.author.name|default:"Anonymous" }}</li>
  {% empty %}
    <li>Nothing filed here yet. Add <code>group: {{ group.slug }}</code> to a post's front matter.</li>
  {% endfor %}
  </ul>

  <h2>Events</h2>
  <ul>
  {% for e in collections.events|in_group:group.slug|upcoming %}
    <li><a href="/events/{{ e.slug }}">{{ e.title }}</a> · {{ e.when|when }}</li>
  {% empty %}
    <li>Nothing coming up.</li>
  {% endfor %}
  </ul>
  {# Signed-in members get their private link, which carries this group's events even when the group isn't public. #}
  <p><friendo-add-to-calendar subscribe group="{{ group.slug }}" label="Subscribe to this group's calendar"></friendo-add-to-calendar></p>

  <h2>Chat</h2>
  {# A group's chats are its members' only. "general" is made the first time this
     page is served; moderators can add more, and each shows up here. #}
  <friendo-chat chat-id="general" group="{{ group.slug }}"></friendo-chat>
  {% for c in group.chats %}{% if c.id != "general" %}
  <h3>{{ c.name }}</h3>
  <friendo-chat chat-id="{{ c.id }}" group="{{ group.slug }}"></friendo-chat>
  {% endif %}{% endfor %}
</article>
{% endblock %}
`

// inboxHTML is a members-only page listing what happened to the viewer: new
// followers, comments on their posts, group requests and invitations.
const inboxHTML = `{% extends "layouts/base.html" %}
{% members only %}
{% block content %}
<h1>Inbox</h1>
<friendo-inbox></friendo-inbox>
{% endblock %}
`

// firstMeetupMD is a starter event, dated a few weeks out so it shows as upcoming.
func firstMeetupMD() string {
	day := time.Now().AddDate(0, 0, 21)
	for day.Weekday() != time.Tuesday {
		day = day.AddDate(0, 0, 1)
	}
	return fmt.Sprintf(`---
title: First meetup
when:
  start: %s 19:00 to 20:30
  repeats: weekly
---

An event is a post with a `+"`when:`"+` line. This one repeats every week — see it
listed under `+"`/events/`"+` and subscribe to `+"`/calendar.ics`"+` from your calendar app.

Other spellings that work:

    when: 2026-10-04                       (all day)
    when: 2026-10-04 10:00 to 16:00
    when: mondays 19:00                    (every Monday from now)
    when: {start: 2026-10-04 19:00, end: 21:00, repeats: monthly, except: [2026-12-23]}
`, day.Format("2006-01-02"))
}

const styleCSS = `/* Friendo starter styles */
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 48rem; margin: 0 auto; padding: 2rem; }
nav { margin-bottom: 2rem; padding-bottom: 1rem; border-bottom: 1px solid #e5e7eb; }
nav a { font-weight: 700; color: #333; text-decoration: none; }
h1 { margin-bottom: 1rem; }
h2 { margin-bottom: 0.5rem; }
article { margin-bottom: 2rem; }
a { color: #2563eb; }
code { background: #f3f4f6; padding: 0.15rem 0.3rem; border-radius: 3px; font-size: 0.9em; }
`
