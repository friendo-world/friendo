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
types = ["blog", "events", "pages"]

# Optional: the fields a type's records carry, so the admin lays out its table and
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
# default_role = "member"        # a new sign-up becomes: "member" or "contributor"
# signups_enabled = true         # allow public sign-ups
# require_approval = false        # hold contributor posts for review before publishing
# accept_submissions = false      # let members submit posts via <friendo-form> (into the review queue)
# auto_approve = false            # publish new comments immediately instead of queuing them
# password_login = false          # also allow signing in with a password (everyone can always use an emailed code)
# comments = true                 # feature switches (Settings → Features): comments, reactions, polls, rsvp, locations, channels
# default_collections = ["blog", "pages", "posts"]   # built-in collections listed while [content] types is unset

# Optional: members-only paths. A page can also gate itself with {%% members only %%}
# (or editors only, admins only …) at the top of its template. Restart to apply.
# [access]
# members_only = ["/members/*"]   # signed-in people only: this path and everything under it
# editors_only = ["/newsroom/*"]  # editors and up

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
    </nav>
    <main>
        {% block content %}{% endblock %}
    </main>
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
    <h1>{{ record.title }}</h1>
    <div>{{ record.body|markdown }}</div>
    <p><a href="/">&larr; Back</a></p>
</article>
{% endblock %}
`

// helloPostMD is a starter content file. Running ` + "`friendo build`" + ` (or
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
    <h1>{{ record.title }}</h1>
    <p>{{ record.when|when }}{% if record.when.repeats %} &middot; {{ record.when.repeats }}{% endif %}</p>
    {% if record.when.repeats and record.when.next %}<p>Next: {{ record.when.next|when }}</p>{% endif %}
    <div>{{ record.body|markdown }}</div>
    <p><a href="{{ record|google_calendar_url }}">Add to Google Calendar</a> &middot;
    <a href="/calendar.ics?record={{ record.id }}">Download .ics</a> &middot; <a href="/events/">All events</a></p>
</article>
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
when: %s 19:00 to 20:30
repeats: weekly
---

An event is a post with a `+"`when:`"+` line. This one repeats every week — see it
listed under `+"`/events/`"+` and subscribe to `+"`/calendar.ics`"+` from your calendar app.

Other spellings that work:

    when: 2026-10-04                       (all day)
    when: 2026-10-04 10:00 to 16:00
    when: mondays 19:00                    (every Monday from now)
    repeats: monthly                       (or: {every: month, on: first tuesday})
    except: [2026-12-23]                   (skip a date)
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
