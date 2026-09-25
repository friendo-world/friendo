/**
 * friendo.js — the community SDK.
 *
 * Drop-in Web Components that add member-gated comments, reactions, and polls to
 * any Friendo site with no custom JavaScript:
 *
 *   <script src="/friendo.js" defer></script>
 *   <friendo-auth></friendo-auth>            (add `reload` to reload the page after sign-in/out —
 *                                             for pages that render {{ user }} or are members-only)
 *   <friendo-comments post-id="…"></friendo-comments>
 *   <friendo-reactions target-type="post" target-id="…"></friendo-reactions>
 *   <friendo-poll poll-id="…"></friendo-poll>
 *   <friendo-map target-type="post" target-id="…"></friendo-map>
 *   <friendo-form collection="posts">…author inputs…</friendo-form>
 *   <friendo-calendar collection="events"></friendo-calendar>   (month grid / list of the site's events)
 *   <friendo-rsvp post-id="…"></friendo-rsvp>                    (going / not going / maybe on an event)
 *   <friendo-add-to-calendar post-id="…"></friendo-add-to-calendar>   (Google / Apple / Outlook / .ics menu; `subscribe` for the whole feed)
 *
 * On a friendo network's own domain, three more tags talk to the network's
 * account API (/api/*) instead of a site's: <friendo-account> (your sites,
 * domains, "Open admin"), <friendo-console> (the operator's levers) and
 * <friendo-activate> (linking `friendo login` from the terminal). The
 * network serves each on a default page (/account, /network, /activate); a
 * home site drops the same tag into its own page to brand it.
 *
 * Every component renders into a shadow root and exposes its internals through
 * `part` attributes, so authors style them from their own stylesheet with
 * ::part() and keep their theme — e.g.
 *
 *   friendo-comments::part(submit) { background: rebeccapurple; color: white; }
 *
 * The components talk only to the runtime-agnostic REST API under /_/api, so the
 * same tag works on a local Go runtime and on the edge.
 */
(function () {
  "use strict";

  var API = "/_/api";

  // Shared JSON fetch. Cookies ride along (the friendo_session cookie is
  // site-wide, so pages can render the viewer and every /_/api call carries it).
  async function api(path, opts) {
    var res = await fetch(API + path, Object.assign({ credentials: "same-origin" }, opts));
    var body = null;
    try {
      body = await res.json();
    } catch (e) {
      /* empty / non-JSON (e.g. 204) */
    }
    if (!res.ok) {
      var err = new Error((body && body.error) || res.statusText);
      err.status = res.status;
      // The site has this feature switched off (Settings → Features): a tag for
      // it renders nothing rather than an error.
      err.off = !!(body && body.off);
      throw err;
    }
    return body;
  }

  function jsonBody(method, data) {
    return { method: method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(data) };
  }

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (ch) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch];
    });
  }

  // Cache of the current member across all components on the page.
  var mePromise = null;
  function currentUser(force) {
    if (force || !mePromise) {
      mePromise = api("/me").then(
        function (r) { return r.user; },
        function () { return null; }
      );
    }
    return mePromise;
  }

  // A logged-in/out change should refresh every component on the page.
  function broadcastAuth(user) {
    mePromise = Promise.resolve(user);
    document.dispatchEvent(new CustomEvent("friendo:auth", { detail: { user: user } }));
  }

  // Base class: a shadow root, a tiny stylesheet, and a re-render on auth changes.
  var BASE_CSS =
    ":host{display:block;font:inherit;color:inherit}" +
    "button{font:inherit;cursor:pointer}" +
    "[hidden]{display:none!important}";

  class FriendoElement extends HTMLElement {
    constructor() {
      super();
      this.attachShadow({ mode: "open" });
      this._onAuth = this._onAuth.bind(this);
    }
    connectedCallback() {
      document.addEventListener("friendo:auth", this._onAuth);
      this.render();
    }
    disconnectedCallback() {
      document.removeEventListener("friendo:auth", this._onAuth);
    }
    _onAuth() {
      this.render();
    }
    // Wrap component markup with the shared reset stylesheet.
    paint(html) {
      this.shadowRoot.innerHTML = "<style>" + BASE_CSS + this.css() + "</style>" + html;
    }
    // A load failed: show why — unless the feature is off for this site, in
    // which case the tag simply isn't there.
    fail(e) {
      if (e && e.off) {
        this.shadowRoot.innerHTML = "";
        this.hidden = true;
        this.setAttribute("data-off", "");
        return;
      }
      this.hidden = false;
      this.paint('<div part="error">' + esc(e.message) + "</div>");
    }
    css() {
      return "";
    }
    render() {}
  }

  // --- <friendo-auth> ------------------------------------------------------
  // Passwordless email + one-time-code login. Emits friendo:auth on state change.
  // With the `reload` attribute it also reloads the page after signing in or out,
  // so server-rendered {{ user }} content and members-only pages catch up.
  class FriendoAuth extends FriendoElement {
    _afterAuthChange(user) {
      broadcastAuth(user);
      if (this.hasAttribute("reload")) location.reload();
    }
    css() {
      return (
        "input{font:inherit;padding:.4em .5em;border:1px solid #ccc;border-radius:6px}" +
        "form{display:flex;gap:.5em;flex-wrap:wrap;align-items:center}" +
        ".status{opacity:.7;font-size:.9em}" +
        ".link{border:0;background:none;padding:0;color:inherit;text-decoration:underline;font:inherit;opacity:.8}" +
        ".panel{margin-top:.5em;font-size:.9em}" +
        ".persona{display:block;width:100%;text-align:left;margin:.2em 0;padding:.35em .5em;border:1px solid #ddd;" +
        "border-radius:8px;background:#fafafa}" +
        '.persona[aria-pressed="true"]{font-weight:600;border-color:#4299e1;background:#e8f0ff}' +
        ".panel form{margin-top:.4em}"
      );
    }
    async render() {
      var user = await currentUser();
      if (user) return this._renderSignedIn(user);
      this._renderRequest();
    }
    // Signed-in view: shows the persona attribution flows to, with a switcher to
    // pick a different persona or add one. The chosen persona is the account's
    // default (server-side), so comments/messages attribute to it everywhere.
    async _renderSignedIn(user) {
      var personas = [];
      try {
        var r = await api("/me/personas");
        personas = (r && r.personas) || [];
      } catch (e) { /* older runtime or no personas — degrade to the account name */ }
      var current = personas.filter(function (p) { return p.is_default; })[0] || personas[0];
      var name = current ? current.name : user.name || user.email;
      var many = personas.length > 1;

      this.paint(
        '<div part="signed-in" class="status">Posting as <b part="name">' + esc(name) + "</b>" +
          ' · <button part="personas-toggle" class="link">personas</button>' +
          ' · <button part="logout">Sign out</button></div>' +
          '<div part="personas" class="panel" hidden></div>'
      );

      var self = this;
      this.shadowRoot.querySelector('[part="logout"]').onclick = async function () {
        await api("/auth/logout", { method: "POST" }).catch(function () {});
        self._afterAuthChange(null);
      };

      var panel = this.shadowRoot.querySelector('[part="personas"]');
      this.shadowRoot.querySelector('[part="personas-toggle"]').onclick = function () {
        panel.hidden = !panel.hidden;
        if (!panel.hidden) self._paintPersonas(panel, personas);
      };
      // If the account already has several personas, open the switcher by default so
      // it's discoverable; a single-persona account keeps it tucked away.
      if (many) { panel.hidden = false; this._paintPersonas(panel, personas); }
    }
    // _paintPersonas fills the switcher panel: a row per persona (click to make it
    // the default) plus an inline "new persona" form.
    _paintPersonas(panel, personas) {
      var self = this;
      var rows = personas
        .map(function (p) {
          return (
            '<button part="persona" class="persona" data-id="' + esc(p.id) + '" aria-pressed="' +
            (p.is_default ? "true" : "false") + '">' + esc(p.name) +
            (p.is_default ? " ✓" : "") + "</button>"
          );
        })
        .join("");
      panel.innerHTML =
        rows +
        '<form part="new-persona"><input part="new-name" placeholder="New persona name" required />' +
        '<button part="add" type="submit">Add</button></form>';

      panel.querySelectorAll(".persona").forEach(function (btn) {
        btn.onclick = async function () {
          if (btn.getAttribute("aria-pressed") === "true") return;
          try {
            await api("/me/personas/" + encodeURIComponent(btn.dataset.id) + "/default", { method: "POST" });
            self.render(); // refresh the "Posting as …" line + panel
          } catch (e) { /* leave as-is */ }
        };
      });

      var form = panel.querySelector("form");
      form.onsubmit = async function (e) {
        e.preventDefault();
        var input = panel.querySelector('[part="new-name"]');
        var name = input.value.trim();
        if (!name) return;
        try {
          await api("/me/personas", jsonBody("POST", { name: name }));
          self.render();
        } catch (err) { /* ignore */ }
      };
    }
    _renderRequest() {
      this.paint(
        '<form part="form"><input part="email" type="email" required placeholder="you@example.com" />' +
          '<button part="button" type="submit">Email me a code</button>' +
          '<span part="status" class="status"></span></form>'
      );
      var form = this.shadowRoot.querySelector("form");
      var status = this.shadowRoot.querySelector('[part="status"]');
      form.onsubmit = async (e) => {
        e.preventDefault();
        var email = this.shadowRoot.querySelector('[part="email"]').value.trim();
        if (!email) return;
        status.textContent = "Sending…";
        try {
          var r = await api("/auth/request-code", jsonBody("POST", { email: email }));
          this._renderVerify(email, r && r.code);
        } catch (err) {
          status.textContent = err.message;
        }
      };
    }
    _renderVerify(email, devCode) {
      this.paint(
        '<form part="form"><input part="code" inputmode="numeric" required placeholder="6-digit code" />' +
          '<button part="button" type="submit">Verify</button>' +
          '<span part="status" class="status">' +
          (devCode ? "Dev code: " + esc(devCode) : "Check " + esc(email)) +
          "</span></form>"
      );
      var form = this.shadowRoot.querySelector("form");
      var status = this.shadowRoot.querySelector('[part="status"]');
      if (devCode) this.shadowRoot.querySelector('[part="code"]').value = devCode;
      form.onsubmit = async (e) => {
        e.preventDefault();
        var code = this.shadowRoot.querySelector('[part="code"]').value.trim();
        status.textContent = "Verifying…";
        try {
          var r = await api("/auth/verify-code", jsonBody("POST", { email: email, code: code }));
          this._afterAuthChange(r.user);
        } catch (err) {
          status.textContent = err.message;
        }
      };
    }
  }

  // --- <friendo-comments post-id> ------------------------------------------
  class FriendoComments extends FriendoElement {
    css() {
      return (
        "ul{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:.75em}" +
        "li{padding:.6em .75em;border:1px solid #eee;border-radius:8px}" +
        'li[data-status="pending"]{border-style:dashed;opacity:.85}' +
        'li[data-status="rejected"]{border-color:#e0b4b4;opacity:.7}' +
        ".meta{font-size:.85em;opacity:.7;margin-bottom:.25em;display:flex;gap:.5em;align-items:center}" +
        ".badge{font-size:.7em;text-transform:uppercase;letter-spacing:.03em;border:1px solid currentColor;" +
        "border-radius:999px;padding:0 .4em;opacity:.8}" +
        ".actions{margin-top:.4em;display:flex;gap:.4em}" +
        ".actions button{font:inherit;font-size:.8em;cursor:pointer;border:1px solid #ddd;border-radius:6px;" +
        "background:#fafafa;padding:.15em .5em}" +
        "textarea{font:inherit;width:100%;box-sizing:border-box;padding:.5em;border:1px solid #ccc;border-radius:6px}" +
        "form{margin-top:.75em;display:flex;flex-direction:column;gap:.5em;align-items:flex-start}" +
        ".empty{opacity:.6}"
      );
    }
    async render() {
      var postId = this.getAttribute("post-id") || "";
      var user = await currentUser();
      var list, comments, canModerate;
      try {
        list = await api("/posts/" + encodeURIComponent(postId) + "/comments");
        comments = (list && list.comments) || [];
        canModerate = !!(list && list.can_moderate);
      } catch (e) {
        this.fail(e);
        return;
      }

      function actions(c) {
        var btns = [];
        if (canModerate && c.status !== "approved") {
          btns.push('<button part="approve" data-act="approved" data-id="' + esc(c.id) + '">Approve</button>');
          btns.push('<button part="reject" data-act="rejected" data-id="' + esc(c.id) + '">Reject</button>');
        }
        if (canModerate || c.mine) {
          btns.push('<button part="delete" data-act="delete" data-id="' + esc(c.id) + '">Delete</button>');
        }
        return btns.length ? '<div class="actions" part="actions">' + btns.join("") + "</div>" : "";
      }

      var items = comments.length
        ? comments
            .map(function (c) {
              var badge = c.status !== "approved"
                ? '<span part="badge" class="badge">' + esc(c.status) + "</span>"
                : "";
              return (
                '<li part="comment" data-status="' + esc(c.status) + '"><div part="author" class="meta">' +
                esc(c.author_name || "Anonymous") + badge +
                '</div><div part="body">' + esc(c.body) + "</div>" + actions(c) + "</li>"
              );
            })
            .join("")
        : '<li part="empty" class="empty">No comments yet.</li>';

      var composer = user
        ? '<form part="form"><textarea part="input" required placeholder="Add a comment…" rows="3"></textarea>' +
          '<button part="submit" type="submit">Post comment</button>' +
          '<span part="status" class="meta"></span></form>'
        : '<p part="signed-out" class="empty">Sign in to join the conversation.</p>';

      this.paint('<ul part="list">' + items + "</ul>" + composer);

      var self = this;
      // Inline moderation / self-delete.
      this.shadowRoot.querySelectorAll(".actions button").forEach(function (btn) {
        btn.onclick = async function () {
          var id = btn.dataset.id;
          try {
            if (btn.dataset.act === "delete") {
              if (!confirm("Delete this comment?")) return;
              await api("/comments/" + encodeURIComponent(id), { method: "DELETE" });
            } else {
              await api("/comments/" + encodeURIComponent(id), jsonBody("PUT", { status: btn.dataset.act }));
            }
            self.render();
          } catch (err) {
            /* leave as-is */
          }
        };
      });

      var form = this.shadowRoot.querySelector("form");
      if (form) {
        var status = this.shadowRoot.querySelector('[part="status"]');
        form.onsubmit = async (e) => {
          e.preventDefault();
          var body = this.shadowRoot.querySelector('[part="input"]').value.trim();
          if (!body) return;
          status.textContent = "Posting…";
          try {
            await api("/posts/" + encodeURIComponent(postId) + "/comments", jsonBody("POST", { body: body }));
            status.textContent = "Submitted for review.";
            this.shadowRoot.querySelector('[part="input"]').value = "";
            self.render();
          } catch (err) {
            status.textContent = err.message;
          }
        };
      }
    }
  }

  // --- <friendo-reactions target-type target-id [emojis]> ------------------
  class FriendoReactions extends FriendoElement {
    css() {
      return (
        ".row{display:flex;gap:.4em;flex-wrap:wrap}" +
        "button{display:inline-flex;gap:.35em;align-items:center;padding:.3em .6em;border:1px solid #ddd;" +
        "border-radius:999px;background:#fafafa}" +
        'button[aria-pressed="true"]{background:#e8f0ff;border-color:#9db8ff}' +
        ".count{font-variant-numeric:tabular-nums;opacity:.75}"
      );
    }
    async render() {
      var type = this.getAttribute("target-type") || "";
      var id = this.getAttribute("target-id") || "";
      var choices = (this.getAttribute("emojis") || "👍,❤️,🎉").split(",").map(function (s) {
        return s.trim();
      });

      var data;
      try {
        data = await api("/reactions?target_type=" + encodeURIComponent(type) + "&target_id=" + encodeURIComponent(id));
      } catch (e) {
        this.fail(e);
        return;
      }
      var byEmoji = {};
      ((data && data.reactions) || []).forEach(function (r) {
        byEmoji[r.emoji] = r;
      });
      // Show configured choices plus any emoji that already has reactions.
      Object.keys(byEmoji).forEach(function (e) {
        if (choices.indexOf(e) === -1) choices.push(e);
      });

      var html = choices
        .map(function (emoji) {
          var r = byEmoji[emoji] || { count: 0, reacted: false };
          return (
            '<button part="button" data-emoji="' +
            esc(emoji) +
            '" aria-pressed="' +
            (r.reacted ? "true" : "false") +
            '"><span part="emoji">' +
            esc(emoji) +
            '</span><span part="count" class="count">' +
            r.count +
            "</span></button>"
          );
        })
        .join("");
      this.paint('<div class="row" part="row">' + html + "</div>");

      var self = this;
      this.shadowRoot.querySelectorAll("button").forEach(function (btn) {
        btn.onclick = async function () {
          var user = await currentUser();
          if (!user) {
            self.dispatchEvent(new CustomEvent("friendo:needs-auth", { bubbles: true }));
            return;
          }
          try {
            await api(
              "/reactions",
              jsonBody("POST", { target_type: type, target_id: id, emoji: btn.dataset.emoji })
            );
            self.render();
          } catch (err) {
            /* surface nothing; leave state as-is */
          }
        };
      });
    }
  }

  // --- <friendo-rsvp post-id [occurrence] [names]> ---------------------------
  // "Are you coming?" on a post that has a `when`. Three answers, one per member
  // per occurrence; the tally is public. On a repeating event the component asks
  // about the next date unless `occurrence` (an RFC 3339 start) names another.
  // The signed-in viewer's answer is the pressed button. Organizers (the post's
  // author, or a moderator) also see who answered when `names` is present.
  // Parts: question, when, row, button, count, mine, names, name, status,
  // signed-out, error.
  class FriendoRSVP extends FriendoElement {
    css() {
      return (
        ".q{margin:0 0 .35em;font-weight:600}.when{opacity:.75;margin:0 0 .5em;font-size:.95em}" +
        ".row{display:flex;gap:.4em;flex-wrap:wrap}" +
        "button{display:inline-flex;gap:.4em;align-items:center;padding:.35em .8em;border:1px solid #ddd;" +
        "border-radius:999px;background:#fafafa}" +
        'button[aria-pressed="true"]{background:#e8f0ff;border-color:#9db8ff}' +
        ".count{font-variant-numeric:tabular-nums;opacity:.75}" +
        ".mine,.status,.signed-out{font-size:.9em;opacity:.75;margin-top:.5em}" +
        ".names{margin:.6em 0 0;padding:0;list-style:none;font-size:.9em}.names li{padding:.1em 0}" +
        ".names .a{opacity:.6;margin-left:.4em}"
      );
    }
    async render() {
      var postId = this.getAttribute("post-id") || "";
      var occ = this.getAttribute("occurrence") || "";
      var q = "/posts/" + encodeURIComponent(postId) + "/rsvps" + (occ ? "?occurrence=" + encodeURIComponent(occ) : "");
      var data;
      try {
        data = await api(q);
      } catch (e) {
        this.fail(e);
        return;
      }
      var user = await currentUser();
      var answers = [
        { key: "going", label: this.getAttribute("going-label") || "Going" },
        { key: "maybe", label: this.getAttribute("maybe-label") || "Maybe" },
        { key: "not_going", label: this.getAttribute("not-going-label") || "Can't go" },
      ];
      var counts = data.counts || {};
      var html =
        '<p part="question" class="q">' + esc(this.getAttribute("question") || "Are you coming?") + "</p>" +
        (data.occurrence_text ? '<p part="when" class="when">' + esc(data.occurrence_text) + "</p>" : "") +
        '<div part="row" class="row">' +
        answers.map(function (a) {
          return '<button part="button" data-answer="' + a.key + '" aria-pressed="' + (data.mine === a.key ? "true" : "false") + '">' +
            esc(a.label) + '<span part="count" class="count">' + (counts[a.key] || 0) + "</span></button>";
        }).join("") +
        "</div>";
      if (!user) {
        html += '<div part="signed-out" class="signed-out">Sign in to answer.</div>';
      } else if (data.mine) {
        html += '<div part="mine" class="mine">You said <b>' + esc(labelFor(answers, data.mine)) + '</b>. <button type="button" part="button" data-clear="1" style="padding:.1em .5em;font-size:.9em">Clear</button></div>';
      }
      if (this.hasAttribute("names") && data.attendees && data.attendees.length) {
        html += '<ul part="names" class="names">' + data.attendees.map(function (r) {
          return '<li part="name">' + esc(r.author_name || "Someone") + '<span class="a">' + esc(labelFor(answers, r.answer)) + "</span></li>";
        }).join("") + "</ul>";
      }
      html += '<div part="status" class="status" hidden></div>';
      this.paint(html);

      var self = this;
      var status = this.shadowRoot.querySelector('[part="status"]');
      this.shadowRoot.querySelectorAll("button[data-answer]").forEach(function (btn) {
        btn.onclick = async function () {
          if (!user) {
            self.dispatchEvent(new CustomEvent("friendo:needs-auth", { bubbles: true }));
            return;
          }
          try {
            await api("/posts/" + encodeURIComponent(postId) + "/rsvps",
              jsonBody("POST", { occurrence: data.occurrence, answer: btn.dataset.answer }));
            self.dispatchEvent(new CustomEvent("friendo:rsvp", { bubbles: true, detail: { postId: postId, occurrence: data.occurrence, answer: btn.dataset.answer } }));
            self.render();
          } catch (err) {
            status.hidden = false;
            status.textContent = err.message;
          }
        };
      });
      var clear = this.shadowRoot.querySelector("button[data-clear]");
      if (clear) {
        clear.onclick = async function () {
          try {
            await api("/posts/" + encodeURIComponent(postId) + "/rsvps?occurrence=" + encodeURIComponent(data.occurrence), { method: "DELETE" });
            self.render();
          } catch (err) {
            status.hidden = false;
            status.textContent = err.message;
          }
        };
      }
    }
  }
  function labelFor(answers, key) {
    for (var i = 0; i < answers.length; i++) if (answers[i].key === key) return answers[i].label;
    return key;
  }

  // --- <friendo-add-to-calendar [post-id] [occurrence] | [subscribe] [collection]> --
  // A small menu of calendar apps. With `post-id`, "add this event": Google
  // Calendar's pre-filled form, and an .ics file for Apple Calendar, Outlook and
  // the rest (one date of a repeating event with `occurrence`). With `subscribe`,
  // the whole feed: Google (add by URL), Apple/Outlook (webcal://), the plain URL.
  // Reads /calendar.json — public, and a plain file in a static export. Parts:
  // button, menu, item, copy, status, error.
  class FriendoAddToCalendar extends FriendoElement {
    css() {
      return (
        ":host{display:inline-block;position:relative}" +
        ".btn{font:inherit;padding:.35em .8em;border:1px solid #ccc;border-radius:999px;background:#fafafa;cursor:pointer}" +
        ".menu{position:absolute;z-index:10;top:calc(100% + .3em);left:0;min-width:14em;margin:0;padding:.3em;list-style:none;" +
          "background:#fff;border:1px solid #ddd;border-radius:8px;box-shadow:0 4px 16px rgba(0,0,0,.12)}" +
        ".menu a,.menu button{display:block;width:100%;text-align:left;font:inherit;padding:.4em .6em;border:0;border-radius:6px;" +
          "background:none;color:inherit;text-decoration:none;cursor:pointer;white-space:nowrap}" +
        ".menu a:hover,.menu button:hover{background:#f2f2f5}" +
        ".status{font-size:.85em;opacity:.7;padding:.2em .6em}"
      );
    }
    async render() {
      var subscribe = this.hasAttribute("subscribe");
      var postId = this.getAttribute("post-id") || "";
      var label = this.getAttribute("label") || (subscribe ? "Subscribe" : "Add to calendar");
      var origin = location.origin;
      var items = [];
      if (subscribe) {
        var coll = this.getAttribute("collection") || "";
        var feed = origin + "/calendar.ics" + (coll ? "?collection=" + encodeURIComponent(coll) : "");
        items = [
          { text: "Google Calendar", href: "https://calendar.google.com/calendar/r?cid=" + encodeURIComponent(feed) },
          { text: "Apple Calendar", href: feed.replace(/^https?:\/\//, "webcal://") },
          { text: "Outlook", href: feed.replace(/^https?:\/\//, "webcal://") },
          { text: "Copy the feed address", copy: feed },
        ];
      } else {
        var q = "/calendar.json?record=" + encodeURIComponent(postId);
        var occ = this.getAttribute("occurrence") || "";
        var ev = null;
        try {
          var res = await fetch(q, { credentials: "same-origin" });
          var body = res.ok ? await res.json() : null;
          var list = (body && body.events) || [];
          ev = occ ? list.filter(function (e) { return e.starts === occ; })[0] : list[0];
        } catch (e) { /* fall through */ }
        if (!ev) {
          this.paint('<div part="error" class="status">No upcoming date to add.</div>');
          return;
        }
        // The Google link for a repeating event carries the rule, so Google repeats it too.
        var google = ev.google;
        if (!occ && ev.rule && google) google += "&recur=" + encodeURIComponent("RRULE:" + ev.rule);
        var ics = occ || !ev.rule ? ev.ics : origin + "/calendar.ics?record=" + encodeURIComponent(postId);
        items = [
          { text: "Google Calendar", href: google },
          { text: "Apple Calendar", href: ics },
          { text: "Outlook", href: ics },
          { text: "Download .ics", href: ics, download: true },
        ];
      }
      this.paint(
        '<button part="button" class="btn" type="button" aria-haspopup="true" aria-expanded="false">' + esc(label) + " ▾</button>" +
        '<ul part="menu" class="menu" hidden>' +
        items.map(function (it) {
          if (it.copy) return '<li><button part="copy" type="button" data-copy="' + esc(it.copy) + '">' + esc(it.text) + "</button></li>";
          return '<li><a part="item" href="' + esc(it.href) + '"' + (it.download ? " download" : ' target="_blank" rel="noopener"') + ">" + esc(it.text) + "</a></li>";
        }).join("") +
        '</ul><div part="status" class="status" hidden></div>'
      );
      var self = this;
      var btn = this.shadowRoot.querySelector('[part="button"]');
      var menu = this.shadowRoot.querySelector('[part="menu"]');
      var status = this.shadowRoot.querySelector('[part="status"]');
      btn.onclick = function () {
        menu.hidden = !menu.hidden;
        btn.setAttribute("aria-expanded", menu.hidden ? "false" : "true");
      };
      this._close = function (e) { if (!self.contains(e.target)) { menu.hidden = true; btn.setAttribute("aria-expanded", "false"); } };
      document.addEventListener("click", this._close);
      var copy = this.shadowRoot.querySelector("[data-copy]");
      if (copy) {
        copy.onclick = async function () {
          try { await navigator.clipboard.writeText(copy.dataset.copy); status.textContent = "Copied. Paste it into your calendar app under “add by URL”."; }
          catch (e) { status.textContent = copy.dataset.copy; }
          status.hidden = false;
        };
      }
    }
    disconnectedCallback() {
      super.disconnectedCallback();
      if (this._close) document.removeEventListener("click", this._close);
    }
  }

  // --- <friendo-poll poll-id> ----------------------------------------------
  class FriendoPoll extends FriendoElement {
    css() {
      return (
        ".q{font-weight:600;margin-bottom:.5em}" +
        ".opt{display:block;width:100%;text-align:left;margin:.3em 0;padding:.5em .6em;border:1px solid #ddd;" +
        "border-radius:8px;background:#fafafa;position:relative;overflow:hidden}" +
        ".bar{position:absolute;inset:0 auto 0 0;background:#e8f0ff;z-index:0}" +
        ".label{position:relative;z-index:1;display:flex;justify-content:space-between;gap:1em}" +
        '.opt[aria-pressed="true"] .label{font-weight:600}' +
        ".total{margin-top:.4em;font-size:.85em;opacity:.7}"
      );
    }
    async render() {
      // Reference a poll by its author-chosen slug (resolved/created server-side)
      // or by a raw id. Either way the response carries the real poll id, which
      // we use to vote.
      var slug = this.getAttribute("poll-slug") || "";
      var pollId = this.getAttribute("poll-id") || "";
      var endpoint = slug
        ? "/polls/by-slug/" + encodeURIComponent(slug)
        : "/polls/" + encodeURIComponent(pollId);
      var data;
      try {
        data = await api(endpoint);
      } catch (e) {
        this.fail(e);
        return;
      }
      var poll = (data && data.poll) || {};
      pollId = poll.id || pollId;
      var total = poll.total_votes || 0;
      var voted = poll.my_vote != null;

      var opts = (poll.options || [])
        .map(function (o) {
          var pct = total ? Math.round((o.votes / total) * 100) : 0;
          var mine = poll.my_vote === o.index;
          return (
            '<button part="option" class="opt" data-index="' +
            o.index +
            '" aria-pressed="' +
            (mine ? "true" : "false") +
            '"' +
            (voted ? " disabled" : "") +
            '><span part="bar" class="bar" style="width:' +
            (voted ? pct : 0) +
            '%"></span><span class="label"><span>' +
            esc(o.text) +
            '</span><span part="result">' +
            (voted ? pct + "% · " + o.votes : "") +
            "</span></span></button>"
          );
        })
        .join("");

      this.paint(
        '<div part="question" class="q">' +
          esc(poll.question) +
          "</div>" +
          opts +
          '<div part="total" class="total">' +
          total +
          (total === 1 ? " vote" : " votes") +
          "</div>"
      );

      if (voted) return;
      var self = this;
      this.shadowRoot.querySelectorAll(".opt").forEach(function (btn) {
        btn.onclick = async function () {
          var user = await currentUser();
          if (!user) {
            self.dispatchEvent(new CustomEvent("friendo:needs-auth", { bubbles: true }));
            return;
          }
          try {
            await api("/polls/" + encodeURIComponent(pollId) + "/vote", jsonBody("POST", { option_index: Number(btn.dataset.index) }));
            self.render();
          } catch (err) {
            /* already voted / closed — re-render to reflect server state */
            self.render();
          }
        };
      });
    }
  }

  // --- <friendo-channel channel-id> ------------------------------------------
  // A community feed: lists a channel's messages and, for signed-in members, a
  // compose box. Members can delete their own messages.
  class FriendoChannel extends FriendoElement {
    css() {
      return (
        "ul{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:.6em}" +
        "li{padding:.5em .7em;border:1px solid #eee;border-radius:10px}" +
        ".meta{font-size:.85em;opacity:.7;margin-bottom:.2em;display:flex;gap:.5em;align-items:center}" +
        ".del{margin-left:auto;font:inherit;font-size:.8em;cursor:pointer;border:0;background:none;opacity:.6}" +
        "textarea{font:inherit;width:100%;box-sizing:border-box;padding:.5em;border:1px solid #ccc;border-radius:6px}" +
        "form{margin-top:.6em;display:flex;flex-direction:column;gap:.5em;align-items:flex-start}" +
        ".empty{opacity:.6}"
      );
    }
    disconnectedCallback() {
      super.disconnectedCallback();
      if (this._es) { this._es.close(); this._es = null; }
    }
    // messageLi builds one message row.
    messageLi(m) {
      var del = m.mine || this._canModerate
        ? '<button part="delete" class="del" data-id="' + esc(m.id) + '">delete</button>'
        : "";
      return (
        '<li part="message" data-id="' + esc(m.id) + '"><div part="author" class="meta">' +
        esc(m.author_name || "Anonymous") + del +
        '</div><div part="body">' + esc(m.body) + "</div></li>"
      );
    }
    async render() {
      var channelId = this.getAttribute("channel-id") || "";
      var user = await currentUser();
      // Editor+ can moderate (delete any); members can delete only their own.
      this._canModerate = !!user && ["editor", "admin", "owner"].includes(user.role);
      var data;
      try {
        data = await api("/channels/" + encodeURIComponent(channelId) + "/messages");
      } catch (e) {
        this.fail(e);
        return;
      }
      var messages = (data && data.messages) || [];
      var self = this;

      var items = messages.length
        ? messages.map(function (m) { return self.messageLi(m); }).join("")
        : '<li part="empty" class="empty">No messages yet.</li>';

      var composer = user
        ? '<form part="form"><textarea part="input" required placeholder="Message…" rows="2"></textarea>' +
          '<button part="submit" type="submit">Send</button><span part="status" class="meta"></span></form>'
        : '<p part="signed-out" class="empty">Sign in to join the conversation.</p>';

      this.paint('<ul part="list">' + items + "</ul>" + composer);
      this.wireActions(channelId);
      this.openStream(channelId);
    }

    // openStream subscribes to live new messages via SSE and appends them.
    openStream(channelId) {
      if (this._es) return; // one connection per element
      var self = this;
      try {
        var es = new EventSource(API + "/channels/" + encodeURIComponent(channelId) + "/stream");
        this._es = es;
        es.onmessage = function (ev) {
          var m;
          try { m = JSON.parse(ev.data); } catch (e) { return; }
          var list = self.shadowRoot.querySelector('[part="list"]');
          if (!list || list.querySelector('[data-id="' + (window.CSS && CSS.escape ? CSS.escape(m.id) : m.id) + '"]')) return;
          var empty = list.querySelector('[part="empty"]');
          if (empty) empty.remove();
          list.insertAdjacentHTML("beforeend", self.messageLi(m));
          self.wireActions(channelId);
        };
        es.onerror = function () { self._streaming = false; };
        this._streaming = true;
      } catch (e) {
        this._streaming = false;
      }
    }

    wireActions(channelId) {
      var self = this;
      this.shadowRoot.querySelectorAll(".del").forEach(function (btn) {
        btn.onclick = async function () {
          if (!confirm("Delete this message?")) return;
          try {
            await api("/messages/" + encodeURIComponent(btn.dataset.id), { method: "DELETE" });
            var li = self.shadowRoot.querySelector('[data-id="' + (window.CSS && CSS.escape ? CSS.escape(btn.dataset.id) : btn.dataset.id) + '"]');
            if (li) li.remove();
          } catch (e) { /* leave as-is */ }
        };
      });
      var form = this.shadowRoot.querySelector("form");
      if (form) {
        var status = this.shadowRoot.querySelector('[part="status"]');
        form.onsubmit = async (e) => {
          e.preventDefault();
          var input = this.shadowRoot.querySelector('[part="input"]');
          var body = input.value.trim();
          if (!body) return;
          status.textContent = "Sending…";
          try {
            await api("/channels/" + encodeURIComponent(channelId) + "/messages", jsonBody("POST", { body: body }));
            input.value = "";
            status.textContent = "";
            // The message arrives back over the SSE stream and is appended there;
            // if streaming is unavailable, re-render to show it.
            if (!self._streaming) self.render();
          } catch (err) { status.textContent = err.message; }
        };
      }
    }
  }

  // --- <friendo-map target-type [target-id] [post-url-pattern]> --------------
  // Renders geo-tags (the locations API) as an interactive map with a marker per
  // location. Two modes:
  //   • with target-id — one target's pins, e.g. <friendo-map target-type="post"
  //     target-id="p1"> shows every location on post p1.
  //   • without target-id — every published post's pin of that type on one map,
  //     e.g. <friendo-map target-type="post">. Each marker links back to its post
  //     using the URL the server resolves; post-url-pattern="/blog/{slug}" is an
  //     override for hosts whose routes the server can't see.
  // Public read — no sign-in needed. Uses Leaflet (open-source, BSD-2) with
  // OpenStreetMap tiles (free, no API key), lazy-loaded from a CDN only on pages
  // that actually use a <friendo-map>, so the rest of the SDK stays lean.
  var LEAFLET_VERSION = "1.9.4";
  var LEAFLET_JS = "https://unpkg.com/leaflet@" + LEAFLET_VERSION + "/dist/leaflet.js";
  var LEAFLET_CSS = "https://unpkg.com/leaflet@" + LEAFLET_VERSION + "/dist/leaflet.css";
  var leafletPromise = null;
  function loadLeaflet() {
    if (leafletPromise) return leafletPromise;
    leafletPromise = new Promise(function (resolve, reject) {
      if (window.L) return resolve(window.L);
      var s = document.createElement("script");
      s.src = LEAFLET_JS;
      s.crossOrigin = "";
      s.onload = function () { window.L ? resolve(window.L) : reject(new Error("map library failed to initialize")); };
      s.onerror = function () { reject(new Error("could not load the map library")); };
      document.head.appendChild(s);
    });
    return leafletPromise;
  }

  // TipTap (ProseMirror) powers <friendo-input type="richtext">'s WYSIWYG editor.
  // Like Leaflet it's lazy-loaded as ES modules from a CDN only on pages that use a
  // richtext field, so the rest of the SDK stays dependency-free. Everything is
  // pinned to one TipTap version (2.27.x) so the extensions share a single ProseMirror
  // instance; `?deps` forces tiptap-markdown (which otherwise wants v3) onto it too.
  var TIPTAP_VERSION = "2.27.2";
  var TIPTAP_MARKDOWN_VERSION = "0.8.10";
  var tiptapPromise = null;
  function loadTiptap() {
    if (tiptapPromise) return tiptapPromise;
    var core = "https://esm.sh/@tiptap/core@" + TIPTAP_VERSION;
    var deps = "?deps=@tiptap/core@" + TIPTAP_VERSION;
    tiptapPromise = Promise.all([
      import(core),
      import("https://esm.sh/@tiptap/starter-kit@" + TIPTAP_VERSION + deps),
      import("https://esm.sh/@tiptap/extension-link@" + TIPTAP_VERSION + deps),
      import("https://esm.sh/@tiptap/extension-placeholder@" + TIPTAP_VERSION + deps),
      import("https://esm.sh/tiptap-markdown@" + TIPTAP_MARKDOWN_VERSION + deps),
    ]).then(function (m) {
      return {
        Editor: m[0].Editor,
        StarterKit: m[1].default,
        Link: m[2].default,
        Placeholder: m[3].default,
        Markdown: m[4].Markdown,
      };
    });
    return tiptapPromise;
  }

  class FriendoMap extends FriendoElement {
    css() {
      // Leaflet needs a definite height on its container at init; aspect-ratio isn't
      // reliably resolved in time, so use a fixed height (override via ::part(map)).
      return (
        ".map{width:100%;height:380px;border:1px solid #ddd;border-radius:10px;background:#eef}" +
        ".empty{opacity:.6}"
      );
    }
    disconnectedCallback() {
      super.disconnectedCallback();
      if (this._map) { this._map.remove(); this._map = null; }
    }
    async render() {
      // Tear down any prior map instance (re-render on auth change) before repainting.
      if (this._map) { this._map.remove(); this._map = null; }

      var targetType = this.getAttribute("target-type") || "";
      var targetId = this.getAttribute("target-id") || "";
      // No target-id → aggregate mode: every published post of this type.
      var q = "/locations?target_type=" + encodeURIComponent(targetType);
      if (targetId) q += "&target_id=" + encodeURIComponent(targetId);
      var data;
      try {
        data = await api(q);
      } catch (e) {
        this.fail(e);
        return;
      }
      var locations = (data && data.locations) || [];
      if (!locations.length) {
        this.paint('<div part="empty" class="empty">No locations yet.</div>');
        return;
      }

      // Leaflet's stylesheet is scoped into the shadow root so it can't leak into
      // the host page; the map container needs a definite size before init.
      this.paint('<link rel="stylesheet" href="' + LEAFLET_CSS + '"><div part="map" class="map"></div>');
      var el = this.shadowRoot.querySelector(".map");

      var L;
      try {
        L = await loadLeaflet();
      } catch (e) {
        this.fail(e);
        return;
      }
      if (!this.isConnected) return; // removed while the library loaded

      var map = L.map(el, { scrollWheelZoom: false });
      this._map = map;
      L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
        maxZoom: 19,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      }).addTo(map);

      // A circle marker per point (SVG — no external icon images, so it renders
      // reliably inside the shadow root). The popup links back to the post when a
      // URL is known (server-resolved url, else post-url-pattern), otherwise it
      // falls back to the plain label — never a broken link.
      var pattern = this.getAttribute("post-url-pattern") || "";
      var pts = locations.map(function (l) {
        var m = L.circleMarker([l.lat, l.lng], { radius: 7, color: "#2b6cb0", fillColor: "#4299e1", fillOpacity: 0.9, weight: 2 }).addTo(map);
        var href = l.url || (pattern && l.slug ? pattern.replace("{slug}", encodeURIComponent(l.slug)) : "");
        var text = l.title || l.label;
        if (href) {
          m.bindPopup('<a href="' + esc(href) + '">' + esc(text || "View post") + "</a>");
        } else if (text) {
          m.bindPopup(esc(text));
        }
        return [l.lat, l.lng];
      });
      if (pts.length === 1) {
        map.setView(pts[0], 13);
      } else {
        map.fitBounds(pts, { padding: [30, 30] });
      }
      // The container may have been laid out after init (aspect-ratio / shadow DOM);
      // recompute the map size so tiles fill it.
      setTimeout(function () { map.invalidateSize(); }, 0);
    }
  }

  // slugify turns a title into a URL-safe slug, matching the server's file-based
  // content convention. Used to derive <friendo-form>'s slug when none is given.
  function slugify(s) {
    return (
      String(s == null ? "" : s)
        .toLowerCase()
        .trim()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "")
        .slice(0, 80) || "post"
    );
  }

  // --- <friendo-input name type [placeholder] [accept]> ----------------------
  // A rich value-provider that <friendo-form> reads via its `.value` getter. Four
  // types, each rendered into its own shadow root and ::part()-styleable:
  //   richtext → a TipTap WYSIWYG editor (CDN-loaded)        (.value = markdown string)
  //   location → a click-to-pick Leaflet map                (.value = {lat,lng} | null)
  //   media    → a file/image picker with preview           (.value = pending File | null)
  //   tags     → a chip input                               (.value = string[])
  // Anything else falls back to a plain text input. The value shape lands in the
  // created post's `data.<name>` (or a reserved column) per <friendo-form>'s rules.
  class FriendoInput extends FriendoElement {
    css() {
      return (
        "input,textarea{font:inherit;padding:.4em .5em;border:1px solid #ccc;border-radius:6px;width:100%;box-sizing:border-box}" +
        ".tb{display:flex;flex-wrap:wrap;gap:.3em;margin-bottom:.4em}" +
        ".tb button{font:inherit;font-size:.85em;min-width:2em;padding:.15em .5em;border:1px solid #ddd;" +
        "border-radius:6px;background:#fafafa;cursor:pointer}" +
        '.tb button[aria-pressed="true"]{background:#e8f0ff;border-color:#9db8ff}' +
        // The editor shell; the ProseMirror surface it hosts carries part=input.
        ".editor{border:1px solid #ccc;border-radius:6px}" +
        ".editor .ProseMirror{min-height:7em;padding:.55em .7em;outline:none}" +
        ".editor .ProseMirror>*{margin:.35em 0}" +
        ".editor .ProseMirror>*:first-child{margin-top:0}" +
        ".editor .ProseMirror ul{padding-left:1.4em;list-style:disc}" +
        ".editor .ProseMirror ol{padding-left:1.4em;list-style:decimal}" +
        ".editor .ProseMirror h2{font-size:1.3em;font-weight:600}" +
        ".editor .ProseMirror h3{font-size:1.1em;font-weight:600}" +
        ".editor .ProseMirror blockquote{border-left:3px solid #ddd;padding-left:.8em;margin-left:0;opacity:.85}" +
        ".editor .ProseMirror code{background:#f2f2f5;padding:.05em .3em;border-radius:4px;font-family:ui-monospace,monospace}" +
        ".editor .ProseMirror a{color:#2b6cb0;text-decoration:underline}" +
        ".editor .ProseMirror p.is-editor-empty:first-child::before{content:attr(data-placeholder);color:#999;float:left;height:0;pointer-events:none}" +
        ".loading{padding:.6em .7em;opacity:.6;font-size:.9em}" +
        ".map{width:100%;height:280px;border:1px solid #ddd;border-radius:10px;background:#eef}" +
        ".coords{font-size:.85em;opacity:.7;margin-top:.3em}" +
        ".preview img{max-width:100%;max-height:220px;margin-top:.4em;border-radius:8px;display:block}" +
        ".note{font-size:.9em;opacity:.7}" +
        ".chips{display:flex;flex-wrap:wrap;gap:.35em;margin-bottom:.35em}" +
        ".chip{display:inline-flex;align-items:center;gap:.3em;padding:.15em .5em;border:1px solid #9db8ff;" +
        "border-radius:999px;background:#e8f0ff;font-size:.9em}" +
        ".chip button{border:0;background:none;font:inherit;cursor:pointer;opacity:.7;padding:0;line-height:1}" +
        ".when{display:grid;gap:.5em;grid-template-columns:repeat(auto-fit,minmax(11em,1fr))}" +
        ".lbl{display:flex;flex-direction:column;gap:.2em;font-size:.9em}.lbl.check{flex-direction:row;align-items:center;gap:.4em;align-self:end}" +
        ".when select{font:inherit;padding:.4em .5em;border:1px solid #ccc;border-radius:6px}"
      );
    }
    get type() {
      return (this.getAttribute("type") || "text").toLowerCase();
    }
    // Read by <friendo-form> at submit time. Shape depends on `type`.
    get value() {
      switch (this.type) {
        case "when":
          return this._whenValue();
        case "location":
          return this._loc || null;
        case "media":
          return this._file || null;
        case "tags":
          return (this._tags || []).slice();
        case "richtext": {
          // The TipTap editor serializes to markdown; fall back to the textarea when
          // the library couldn't load.
          if (this._editor && this._editor.storage && this._editor.storage.markdown) {
            return (this._editor.storage.markdown.getMarkdown() || "").trim();
          }
          var ta = this.shadowRoot.querySelector('[part="fallback"]');
          return ta ? ta.value.trim() : "";
        }
        default: {
          var i = this.shadowRoot.querySelector("input,textarea");
          return i ? i.value.trim() : "";
        }
      }
    }
    // Clear the field back to empty (called by <friendo-form> after a submit).
    reset() {
      this._loc = null;
      this._file = null;
      this._tags = [];
      this._when = null;
      // Keep the mounted TipTap editor; just empty it (a full re-render would reload
      // the library). Other types re-render from their now-cleared state.
      if (this.type === "richtext" && this._editor) {
        this._editor.commands.clearContent();
        return;
      }
      this.render();
    }
    // Auth changes only matter to the media picker (a contributor+ affordance);
    // ignoring them elsewhere preserves in-progress input across a login/logout.
    _onAuth() {
      if (this.type === "media") this.render();
    }
    disconnectedCallback() {
      super.disconnectedCallback();
      if (this._map) { this._map.remove(); this._map = null; }
      if (this._editor) { this._editor.destroy(); this._editor = null; }
    }
    render() {
      switch (this.type) {
        case "when": return this._renderWhen();
        case "richtext": return this._renderRichtext();
        case "location": return this._renderLocation();
        case "media": return this._renderMedia();
        case "tags": return this._renderTags();
        default: return this._renderText();
      }
    }
    _renderText() {
      this.paint('<input part="input" placeholder="' + esc(this.getAttribute("placeholder") || "") + '">');
    }
    // One control for an event's time: start, end, all-day, and how it repeats.
    // Its value is the flat {when, ends, all_day, repeats} shape the server reads
    // (the same keys as front matter), which <friendo-form> spreads into the post.
    _renderWhen() {
      var self = this;
      this.paint(
        '<div part="when" class="when">' +
          '<label part="label" class="lbl"><span>Starts</span><input part="input" data-k="when" type="datetime-local"></label>' +
          '<label part="label" class="lbl"><span>Ends</span><input part="input" data-k="ends" type="datetime-local"></label>' +
          '<label part="label" class="lbl check"><input part="checkbox" data-k="all_day" type="checkbox"><span>All day</span></label>' +
          '<label part="label" class="lbl"><span>Repeats</span><select part="select" data-k="repeats">' +
            '<option value="">Never</option><option value="daily">Daily</option><option value="weekly">Weekly</option>' +
            '<option value="every 2 weeks">Every 2 weeks</option><option value="monthly">Monthly</option><option value="yearly">Yearly</option>' +
          "</select></label>" +
          '<label part="label" class="lbl until" hidden><span>Until</span><input part="input" data-k="until" type="date"></label>' +
        "</div>"
      );
      var root = this.shadowRoot;
      var allDay = root.querySelector('[data-k="all_day"]');
      var repeats = root.querySelector('[data-k="repeats"]');
      var until = root.querySelector(".until");
      function sync() {
        // All-day events take dates, timed ones take date-times.
        var t = allDay.checked ? "date" : "datetime-local";
        ["when", "ends"].forEach(function (k) {
          var i = root.querySelector('[data-k="' + k + '"]');
          if (i.type !== t) { var v = i.value; i.type = t; i.value = allDay.checked ? v.slice(0, 10) : (v ? v + (v.length === 10 ? "T09:00" : "") : ""); }
        });
        until.hidden = !repeats.value;
      }
      allDay.onchange = sync;
      repeats.onchange = sync;
      // Restore a prior value (a re-render on auth change keeps what was typed).
      var w = this._when || {};
      root.querySelector('[data-k="when"]').value = w.when || "";
      root.querySelector('[data-k="ends"]').value = w.ends || "";
      allDay.checked = !!w.all_day;
      repeats.value = w.repeats || "";
      root.querySelector('[data-k="until"]').value = w.until || "";
      sync();
      root.querySelectorAll("[data-k]").forEach(function (i) {
        i.addEventListener("input", function () { self._when = self._readWhen(); });
        i.addEventListener("change", function () { self._when = self._readWhen(); });
      });
    }
    _readWhen() {
      var root = this.shadowRoot;
      var get = function (k) { var i = root.querySelector('[data-k="' + k + '"]'); return i ? (i.type === "checkbox" ? i.checked : i.value) : ""; };
      return { when: get("when"), ends: get("ends"), all_day: get("all_day"), repeats: get("repeats"), until: get("until") };
    }
    _whenValue() {
      var w = this.shadowRoot.querySelector('[data-k="when"]') ? this._readWhen() : (this._when || {});
      if (!w.when) return null;
      var out = { when: w.when.replace("T", " ") };
      if (w.ends) out.ends = w.ends.replace("T", " ");
      if (w.all_day) out.all_day = true;
      if (w.repeats) {
        if (w.until) {
          var m = /^(?:every (\d+) )?(daily|weekly|monthly|yearly|weeks?)$/.exec(w.repeats);
          var unit = { daily: "day", weekly: "week", monthly: "month", yearly: "year" }[w.repeats] || "week";
          var every = m && m[1] ? m[1] + " " + unit + "s" : unit;
          out.repeats = { every: every, until: w.until };
        } else {
          out.repeats = w.repeats;
        }
      }
      return out;
    }
    // A TipTap (ProseMirror) WYSIWYG editor: formatting shows live in the field, and
    // `.value` serializes to markdown (via tiptap-markdown) so it still flows through
    // the server's markdown filter and fits the `body` column. TipTap is lazy-loaded
    // from a CDN; if that fails, degrade to a plain markdown textarea.
    async _renderRichtext() {
      var ph = this.getAttribute("placeholder") || "";
      this.paint(
        '<div part="toolbar" class="tb"></div>' +
          '<div part="editor" class="editor"><div class="loading">Loading editor…</div></div>'
      );
      var toolbar = this.shadowRoot.querySelector('[part="toolbar"]');
      var mount = this.shadowRoot.querySelector('[part="editor"]');
      var T;
      try {
        T = await loadTiptap();
      } catch (e) {
        return this._renderRichtextFallback(ph);
      }
      if (!this.isConnected) return;
      mount.innerHTML = "";

      var self = this;
      var editor = new T.Editor({
        element: mount,
        extensions: [
          T.StarterKit,
          T.Link.configure({ openOnClick: false, autolink: true }),
          T.Placeholder.configure({ placeholder: ph }),
          T.Markdown,
        ],
        content: this._md || "",
        editorProps: { attributes: { part: "input", role: "textbox", "aria-multiline": "true" } },
      });
      this._editor = editor;

      // Toolbar: label, the chain command to toggle, and the state to reflect as
      // pressed. Link is special (it prompts for a URL).
      var tools = [
        { key: "bold", label: "B", title: "Bold", cmd: function (c) { return c.toggleBold(); }, on: ["bold"] },
        { key: "italic", label: "I", title: "Italic", cmd: function (c) { return c.toggleItalic(); }, on: ["italic"] },
        { key: "h2", label: "H2", title: "Heading", cmd: function (c) { return c.toggleHeading({ level: 2 }); }, on: ["heading", { level: 2 }] },
        { key: "h3", label: "H3", title: "Subheading", cmd: function (c) { return c.toggleHeading({ level: 3 }); }, on: ["heading", { level: 3 }] },
        { key: "bullet", label: "• List", title: "Bullet list", cmd: function (c) { return c.toggleBulletList(); }, on: ["bulletList"] },
        { key: "ordered", label: "1. List", title: "Numbered list", cmd: function (c) { return c.toggleOrderedList(); }, on: ["orderedList"] },
        { key: "quote", label: "❝", title: "Quote", cmd: function (c) { return c.toggleBlockquote(); }, on: ["blockquote"] },
        { key: "code", label: "</>", title: "Inline code", cmd: function (c) { return c.toggleCode(); }, on: ["code"] },
        { key: "link", label: "🔗", title: "Link", cmd: null, on: ["link"] },
      ];
      this._tools = tools;
      toolbar.innerHTML = tools
        .map(function (t) {
          return '<button type="button" part="tool" data-k="' + t.key + '" title="' + esc(t.title) + '" aria-pressed="false">' + esc(t.label) + "</button>";
        })
        .join("");
      toolbar.querySelectorAll("button").forEach(function (btn) {
        var t = tools.filter(function (x) { return x.key === btn.dataset.k; })[0];
        btn.onmousedown = function (e) { e.preventDefault(); }; // keep the selection
        btn.onclick = function () {
          if (t.key === "link") return self._promptLink(editor);
          t.cmd(editor.chain().focus()).run();
        };
      });

      var sync = function () { self._syncTools(toolbar, editor); };
      editor.on("selectionUpdate", sync);
      editor.on("transaction", sync);
      sync();
    }
    // Reflect the caret's active marks/nodes as pressed toolbar buttons.
    _syncTools(toolbar, editor) {
      (this._tools || []).forEach(function (t) {
        var btn = toolbar.querySelector('[data-k="' + t.key + '"]');
        if (!btn) return;
        var active = t.on.length > 1 ? editor.isActive(t.on[0], t.on[1]) : editor.isActive(t.on[0]);
        btn.setAttribute("aria-pressed", active ? "true" : "false");
      });
    }
    _promptLink(editor) {
      if (editor.isActive("link")) { editor.chain().focus().unsetLink().run(); return; }
      var url = prompt("Link URL", "https://");
      if (url == null || url === "") return;
      editor.chain().focus().setLink({ href: url }).run();
    }
    // Fallback when the TipTap CDN can't be reached: a plain markdown textarea.
    _renderRichtextFallback(ph) {
      this.paint(
        '<textarea part="fallback" class="fallback" placeholder="' + esc(ph) + '"></textarea>' +
          '<div part="note" class="note">Rich editor unavailable — write Markdown here.</div>'
      );
    }
    async _renderLocation() {
      if (this._map) { this._map.remove(); this._map = null; }
      this.paint(
        '<link rel="stylesheet" href="' + LEAFLET_CSS + '">' +
          '<div part="map" class="map"></div><div part="coords" class="coords">Click the map to drop a pin.</div>'
      );
      var el = this.shadowRoot.querySelector(".map");
      var coords = this.shadowRoot.querySelector('[part="coords"]');
      var L;
      try {
        L = await loadLeaflet();
      } catch (e) {
        this.fail(e);
        return;
      }
      if (!this.isConnected) return;
      var map = L.map(el, { scrollWheelZoom: false }).setView([20, 0], 1);
      this._map = map;
      L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
        maxZoom: 19,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      }).addTo(map);
      // Restore a prior pick (e.g. after a re-render); SVG circle marker so no
      // external icon image is needed inside the shadow root.
      var self = this;
      var marker = null;
      function place(latlng) {
        // When the map is zoomed out enough that the world repeats horizontally, a
        // click can yield a longitude outside [-180, 180]; wrap it (and clamp the
        // latitude) so the stored value and the geo-tag stay in valid ranges.
        var w = latlng.wrap ? latlng.wrap() : latlng;
        var lat = Math.max(-90, Math.min(90, w.lat));
        self._loc = { lat: +lat.toFixed(6), lng: +w.lng.toFixed(6) };
        coords.textContent = self._loc.lat + ", " + self._loc.lng;
        if (marker) marker.setLatLng(w);
        else marker = L.circleMarker(w, { radius: 8, color: "#2b6cb0", fillColor: "#4299e1", fillOpacity: 0.9, weight: 2 }).addTo(map);
      }
      if (this._loc) { place(L.latLng(this._loc.lat, this._loc.lng)); map.setView([this._loc.lat, this._loc.lng], 10); }
      map.on("click", function (e) { place(e.latlng); });
      setTimeout(function () { map.invalidateSize(); }, 0);
    }
    async _renderMedia() {
      // First cut: media is a contributor+ affordance. Members see a note instead
      // of a picker, so a member submission simply carries no file.
      var user = await currentUser();
      var canUpload = !!user && ["contributor", "editor", "admin", "owner"].indexOf(user.role) !== -1;
      if (!canUpload) {
        this._file = null;
        this.paint('<div part="note" class="note">Sign in as a contributor to attach media.</div>');
        return;
      }
      var accept = this.getAttribute("accept") || "image/*";
      this.paint('<input part="file" type="file" accept="' + esc(accept) + '"><div part="preview" class="preview"></div>');
      var input = this.shadowRoot.querySelector('[part="file"]');
      var preview = this.shadowRoot.querySelector('[part="preview"]');
      var self = this;
      input.onchange = function () {
        var f = input.files && input.files[0];
        self._file = f || null;
        preview.innerHTML = "";
        if (f && /^image\//.test(f.type)) {
          var img = document.createElement("img");
          img.src = URL.createObjectURL(f);
          preview.appendChild(img);
        } else if (f) {
          preview.textContent = f.name;
        }
      };
    }
    _renderTags() {
      if (!this._tags) this._tags = [];
      var ph = this.getAttribute("placeholder") || "Add a tag, then Enter";
      this.paint('<div part="chips" class="chips"></div><input part="input" placeholder="' + esc(ph) + '">');
      this._paintChips();
      var input = this.shadowRoot.querySelector('[part="input"]');
      var self = this;
      input.onkeydown = function (e) {
        if (e.key === "Enter" || e.key === ",") {
          e.preventDefault();
          var v = input.value.trim().replace(/,+$/, "").trim();
          if (v && self._tags.indexOf(v) === -1) { self._tags.push(v); self._paintChips(); }
          input.value = "";
        } else if (e.key === "Backspace" && !input.value && self._tags.length) {
          self._tags.pop();
          self._paintChips();
        }
      };
    }
    _paintChips() {
      var chips = this.shadowRoot.querySelector('[part="chips"]');
      if (!chips) return;
      var self = this;
      chips.innerHTML = this._tags
        .map(function (t, i) {
          return (
            '<span part="chip" class="chip">' + esc(t) +
            '<button type="button" part="chip-remove" data-i="' + i + '" aria-label="Remove ' + esc(t) + '">×</button></span>'
          );
        })
        .join("");
      chips.querySelectorAll("button").forEach(function (b) {
        b.onclick = function () { self._tags.splice(+b.dataset.i, 1); self._paintChips(); };
      });
    }
  }

  // --- <friendo-form collection [redirect] [status]> -------------------------
  // Turns the author's own inputs into a created post, submitted from the page.
  // Unlike every other component this is a LIGHT-DOM controller — no shadow root —
  // so the author's inputs are its real children: their CSS applies, their
  // <friendo-input>s are reachable, and a light-DOM `<button type="submit">` (or
  // Enter in a single-line input) drives it. Field names decide where each value
  // lands: `title`/`body`/`slug`/`status` become post columns; every other name
  // becomes metadata under `data.<name>` (read server-side as {{ record.data.name }}).
  class FriendoForm extends HTMLElement {
    connectedCallback() {
      // A thin status/error line appended after the author's markup. It carries
      // `part` attributes for consistency with the shadow components and, being
      // light DOM, is also directly selectable (friendo-form [part="error"]).
      this._status = document.createElement("div");
      this._status.setAttribute("part", "status");
      this._status.style.cssText = "font-size:.9em;opacity:.75;margin-top:.4em";
      this._error = document.createElement("div");
      this._error.setAttribute("part", "error");
      this._error.style.cssText = "font-size:.9em;color:#b03030;margin-top:.4em";
      this.appendChild(this._status);
      this.appendChild(this._error);
      this._onClick = this._onClick.bind(this);
      this._onKey = this._onKey.bind(this);
      this.addEventListener("click", this._onClick);
      this.addEventListener("keydown", this._onKey);
    }
    disconnectedCallback() {
      this.removeEventListener("click", this._onClick);
      this.removeEventListener("keydown", this._onKey);
    }
    // A submit button lives in the author's light DOM, so clicks inside a
    // <friendo-input> shadow root retarget to the host (not a <button>) and are
    // ignored — only the real submit button matches.
    _onClick(e) {
      var btn = e.target.closest && e.target.closest("button");
      if (!btn) return;
      if ((btn.getAttribute("type") || "submit").toLowerCase() === "submit") {
        e.preventDefault();
        this.submit();
      }
    }
    _onKey(e) {
      // Enter submits from a single-line native input; textareas and the richtext
      // editor (retargeted to the friendo-input host) keep their newlines.
      if (e.key === "Enter" && e.target.tagName === "INPUT") {
        e.preventDefault();
        this.submit();
      }
    }
    async submit() {
      if (this._busy) return;
      this._error.textContent = "";
      var reserved = { title: 1, body: 1, slug: 1, status: 1 };
      var columns = {};
      var data = {};
      var media = [];
      this.querySelectorAll("[name]").forEach(function (el) {
        var name = el.getAttribute("name");
        if (!name) return;
        var tag = el.tagName.toLowerCase();
        if (tag === "friendo-input") {
          var itype = (el.getAttribute("type") || "").toLowerCase();
          if (itype === "media") {
            media.push({ name: name, el: el });
            return;
          }
          if (itype === "when") {
            // The when control yields the reserved calendar keys themselves
            // (when, ends, all_day, repeats); the server lifts them into the event.
            var wv = el.value;
            if (wv) Object.keys(wv).forEach(function (k) { data[k] = wv[k]; });
            return;
          }
          // A location lands in data.<name> as {lat,lng}; the server turns any such
          // field into a geo-tag on create, so <friendo-map> surfaces the pin.
          var rv = el.value;
          if (reserved[name]) columns[name] = typeof rv === "string" ? rv : String(rv == null ? "" : rv);
          else if (rv != null && rv !== "" && !(Array.isArray(rv) && !rv.length)) data[name] = rv;
          return;
        }
        var val = tag === "input" && el.type === "checkbox" ? el.checked : el.value;
        if (reserved[name]) columns[name] = val;
        else if (val != null && val !== "") data[name] = val;
      });
      if (!columns.slug && columns.title) columns.slug = slugify(columns.title);

      var collection = this.getAttribute("collection") || "posts";
      var payload = {
        title: columns.title || "",
        body: columns.body || "",
        slug: columns.slug || "",
        status: columns.status || this.getAttribute("status") || "published",
        data: data,
      };

      this._busy = true;
      this._status.textContent = "Submitting…";
      var record;
      try {
        var res = await api("/collections/" + encodeURIComponent(collection) + "/records", jsonBody("POST", payload));
        record = res.record;
      } catch (err) {
        this._busy = false;
        this._status.textContent = "";
        // Turn the bare API errors into something a visitor can act on.
        if (err.status === 401) {
          this._error.textContent = "Please sign in before posting.";
        } else if (err.status === 403) {
          this._error.textContent =
            "Your account can't post here. Contributors can post directly; members can post only if the site has submissions turned on.";
        } else {
          this._error.textContent = err.message;
        }
        return;
      }

      // Media is async and record-scoped, so it follows creation: upload each
      // pending file to the new record, then patch its URL into the record's data.
      var patched = false;
      for (var i = 0; i < media.length; i++) {
        var file = media[i].el.value;
        if (!file) continue;
        try {
          var fd = new FormData();
          fd.append("record_type", "post");
          fd.append("record_id", record.id);
          fd.append("field", media[i].name);
          fd.append("file", file);
          var up = await api("/files", { method: "POST", body: fd });
          // Write back what was submitted plus the asset — not only the server's
          // copy of `data`, which has the calendar keys (when, repeats…) lifted out.
          record.data = Object.assign({}, data, record.data || {});
          record.data[media[i].name] = up.file.url;
          patched = true;
        } catch (err) {
          /* the post exists; the asset just didn't attach */
        }
      }
      if (patched) {
        try {
          var saved = await api("/records/" + encodeURIComponent(record.id), jsonBody("PUT", {
            title: record.title, body: record.body, slug: record.slug, status: record.status, data: record.data,
          }));
          // Hand listeners the server's record (data with the calendar keys lifted
          // out, `when` filled in), not our working copy.
          if (saved && saved.record) record = saved.record;
        } catch (err) { /* leave the record without the asset ref */ }
      }

      this._busy = false;
      this._status.textContent = "";
      this.dispatchEvent(new CustomEvent("friendo:submitted", { bubbles: true, detail: { record: record } }));

      var redirect = this.getAttribute("redirect");
      if (redirect) {
        location.assign(
          redirect.replace("{slug}", encodeURIComponent(record.slug || "")).replace("{id}", encodeURIComponent(record.id))
        );
        return;
      }
      this._reset();
      this._status.textContent = "Submitted.";
    }
    _reset() {
      this.querySelectorAll("input,textarea,select").forEach(function (el) {
        if (el.type === "checkbox" || el.type === "radio") el.checked = false;
        else el.value = "";
      });
      this.querySelectorAll("friendo-input").forEach(function (el) { if (el.reset) el.reset(); });
    }
  }


  // --- <friendo-calendar [collection] [view=month|list] [month=YYYY-MM] [limit]> --
  // The site's events from /calendar.json (public; also a plain file in a static
  // export). `month` view is a grid with prev/next; `list` is the upcoming
  // occurrences grouped by day. Each entry links to its post. Parts: nav, title,
  // button, view, grid, weekday, day, today, outside, date, event, more, list,
  // group, heading, time, empty, error.
  class FriendoCalendar extends FriendoElement {
    css() {
      return (
        ".nav{display:flex;align-items:center;gap:.5em;margin-bottom:.6em}" +
        ".nav h2{font-size:1.1em;margin:0;flex:1}" +
        ".nav button{font:inherit;padding:.25em .7em;border:1px solid #ccc;border-radius:6px;background:#fafafa;cursor:pointer}" +
        '.nav button[aria-pressed="true"]{background:#e8f0ff;border-color:#9db8ff}' +
        ".grid{display:grid;grid-template-columns:repeat(7,1fr);gap:1px;background:#ddd;border:1px solid #ddd}" +
        ".wd{background:#f4f4f6;font-size:.8em;text-align:center;padding:.3em;opacity:.8}" +
        ".day{background:#fff;min-height:5.5em;padding:.3em;font-size:.85em;display:flex;flex-direction:column;gap:.15em}" +
        ".day.outside{background:#fafafa;opacity:.55}" +
        ".day.today .date{font-weight:700;text-decoration:underline}" +
        ".date{opacity:.7;font-size:.9em}" +
        ".ev{display:block;padding:.1em .3em;border-radius:4px;background:#e8f0ff;color:inherit;text-decoration:none;" +
          "overflow:hidden;text-overflow:ellipsis;white-space:nowrap}" +
        ".ev:hover{background:#d5e3ff}" +
        ".more{font-size:.85em;opacity:.7}" +
        ".list{list-style:none;padding:0;margin:0}" +
        ".group{margin:0 0 .9em}.group h3{font-size:.95em;margin:0 0 .25em}" +
        ".group li{display:flex;gap:.6em;padding:.15em 0}.time{opacity:.7;min-width:6.5em}" +
        ".empty,.error{opacity:.7;padding:.5em 0}"
      );
    }
    connectedCallback() {
      this._view = (this.getAttribute("view") || "month").toLowerCase();
      var m = /^(\d{4})-(\d{2})$/.exec(this.getAttribute("month") || "");
      var now = new Date();
      this._year = m ? +m[1] : now.getFullYear();
      this._month = m ? +m[2] - 1 : now.getMonth();
      // A <friendo-form> on the same page just made a post: refetch, and open
      // the month of its event so the new entry is in view.
      var self = this;
      this._onSubmitted = function (e) {
        var rec = e.detail && e.detail.record;
        self._events = null;
        if (rec && rec.when && rec.when.starts && rec.status === "published") {
          var d = new Date(rec.when.starts);
          if (!isNaN(d)) { self._year = d.getFullYear(); self._month = d.getMonth(); }
        }
        self.render();
      };
      document.addEventListener("friendo:submitted", this._onSubmitted);
      super.connectedCallback();
    }
    disconnectedCallback() {
      super.disconnectedCallback();
      document.removeEventListener("friendo:submitted", this._onSubmitted);
    }
    async _load() {
      // Fetch once per mount: the JSON covers up to two years, and the view slices it.
      if (this._events) return this._events;
      var q = "/calendar.json?from=" + encodeURIComponent(ymd(new Date(this._year, this._month - 1, 1))) +
        "&to=" + encodeURIComponent(ymd(new Date(this._year + 2, this._month, 1)));
      var coll = this.getAttribute("collection");
      if (coll) q += "&collection=" + encodeURIComponent(coll);
      var res = await fetch(q, { credentials: "same-origin" });
      if (!res.ok) throw new Error("Couldn't load the calendar (" + res.status + ")");
      var body = await res.json();
      this._events = (body && body.events) || [];
      return this._events;
    }
    async render() {
      var events;
      try {
        events = await this._load();
      } catch (e) {
        this.paint('<div part="error" class="error">' + esc(e.message) + "</div>");
        return;
      }
      if (!this.isConnected) return;
      var self = this;
      var title = this._view === "month"
        ? new Date(this._year, this._month, 1).toLocaleDateString(undefined, { month: "long", year: "numeric" })
        : "Upcoming";
      var html =
        '<div part="nav" class="nav">' +
          (this._view === "month"
            ? '<button part="button" type="button" data-nav="-1" aria-label="Previous month">&lsaquo;</button>' +
              '<button part="button" type="button" data-nav="1" aria-label="Next month">&rsaquo;</button>'
            : "") +
          '<h2 part="title">' + esc(title) + "</h2>" +
          '<button part="view" type="button" data-view="month" aria-pressed="' + (this._view === "month") + '">Month</button>' +
          '<button part="view" type="button" data-view="list" aria-pressed="' + (this._view === "list") + '">List</button>' +
        "</div>" +
        (this._view === "month" ? this._monthHTML(events) : this._listHTML(events));
      this.paint(html);
      this.shadowRoot.querySelectorAll("[data-nav]").forEach(function (b) {
        b.onclick = function () {
          var d = new Date(self._year, self._month + (+b.dataset.nav), 1);
          self._year = d.getFullYear(); self._month = d.getMonth();
          self.render();
        };
      });
      this.shadowRoot.querySelectorAll("[data-view]").forEach(function (b) {
        b.onclick = function () { self._view = b.dataset.view; self.render(); };
      });
    }
    _monthHTML(events) {
      var first = new Date(this._year, this._month, 1);
      var start = new Date(first); start.setDate(1 - first.getDay()); // week starts Sunday
      var today = ymd(new Date());
      var byDay = {};
      events.forEach(function (e) {
        var key = dayKey(e);
        (byDay[key] = byDay[key] || []).push(e);
      });
      var html = '<div part="grid" class="grid">';
      ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"].forEach(function (d) {
        html += '<div part="weekday" class="wd">' + d + "</div>";
      });
      for (var i = 0; i < 42; i++) {
        var d = new Date(start); d.setDate(start.getDate() + i);
        if (i >= 35 && d.getMonth() !== this._month) break;
        var key = ymd(d);
        var outside = d.getMonth() !== this._month;
        var cls = "day" + (outside ? " outside" : "") + (key === today ? " today" : "");
        var part = "day" + (key === today ? " today" : "") + (outside ? " outside" : "");
        html += '<div part="' + part + '" class="' + cls + '" data-date="' + key + '"><span part="date" class="date">' + d.getDate() + "</span>";
        var list = byDay[key] || [];
        list.slice(0, 3).forEach(function (e) { html += eventLink(e, true); });
        if (list.length > 3) html += '<span part="more" class="more">+' + (list.length - 3) + " more</span>";
        html += "</div>";
      }
      return html + "</div>";
    }
    _listHTML(events) {
      var limit = +(this.getAttribute("limit") || 0);
      var today = ymd(new Date());
      var upcoming = events.filter(function (e) { return dayKey(e) >= today; });
      if (limit > 0) upcoming = upcoming.slice(0, limit);
      if (!upcoming.length) return '<div part="empty" class="empty">Nothing coming up.</div>';
      var groups = [], last = null;
      upcoming.forEach(function (e) {
        var key = dayKey(e);
        if (!last || last.key !== key) { last = { key: key, items: [] }; groups.push(last); }
        last.items.push(e);
      });
      return '<div part="list" class="list">' + groups.map(function (g) {
        var d = new Date(g.key + "T12:00:00");
        return '<div part="group" class="group"><h3 part="heading">' +
          esc(d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" })) + "</h3><ul class=\"list\">" +
          g.items.map(function (e) {
            return '<li><span part="time" class="time">' + esc(timeOf(e)) + "</span>" + eventLink(e, false) + "</li>";
          }).join("") + "</ul></div>";
      }).join("") + "</div>";
    }
  }
  function pad2(n) { return (n < 10 ? "0" : "") + n; }
  function ymd(d) { return d.getFullYear() + "-" + pad2(d.getMonth() + 1) + "-" + pad2(d.getDate()); }
  // An all-day event lives on its calendar date wherever the viewer is; a timed
  // one is bucketed by the viewer's local day.
  function dayKey(e) {
    if (e.all_day) return String(e.starts).slice(0, 10);
    return ymd(new Date(e.starts));
  }
  function timeOf(e) {
    if (e.all_day) return "All day";
    var d = new Date(e.starts);
    return d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  }
  function eventLink(e, short) {
    var label = (short && !e.all_day ? timeOf(e) + " " : "") + (e.title || "");
    if (e.url) return '<a part="event" class="ev" href="' + esc(e.url) + '" title="' + esc(e.title || "") + '">' + esc(label) + "</a>";
    return '<span part="event" class="ev">' + esc(label) + "</span>";
  }

  // --- network: account, console, activate ----------------------------------
  // These talk to the network's own API on its bare domain (/api/*), not a
  // site's /_/api. The network account cookie rides along.
  var NAPI = "/api";
  async function napi(path, opts) {
    var res = await fetch(NAPI + path, Object.assign({ credentials: "same-origin" }, opts));
    var body = null;
    try { body = await res.json(); } catch (e) { /* 204 / non-JSON */ }
    if (!res.ok) {
      var err = new Error((body && body.error) || res.statusText);
      err.status = res.status;
      err.body = body;
      throw err;
    }
    return body;
  }
  var accountPromise = null;
  function currentAccount(force) {
    if (force || !accountPromise) {
      accountPromise = napi("/account").then(function (a) { return a; }, function () { return null; });
    }
    return accountPromise;
  }
  function broadcastAccount(acct) {
    accountPromise = Promise.resolve(acct);
    document.dispatchEvent(new CustomEvent("friendo:account", { detail: { account: acct } }));
  }
  // A site address as a link from wherever this page is: same scheme, and the
  // same port when the network runs on one (demo.localhost:3000 in dev).
  function siteHref(address) {
    return location.protocol + "//" + address + (location.port ? ":" + location.port : "") + "/";
  }
  function confirmed(msg) { return typeof window.confirm !== "function" || window.confirm(msg); }

  var NET_CSS =
    "h2{font-size:1.05em;margin:1.4em 0 .4em}h3{font-size:1em;margin:1em 0 .3em}" +
    "table{border-collapse:collapse;width:100%;font-size:.95em}th,td{text-align:left;padding:.4em .5em;border-bottom:1px solid #e5e7eb;vertical-align:top}" +
    "th{font-weight:600;font-size:.85em;opacity:.7}" +
    "input,select{font:inherit;padding:.4em .5em;border:1px solid #ccc;border-radius:6px;max-width:100%}" +
    "button{font:inherit;padding:.35em .7em;border:1px solid #d1d5db;border-radius:6px;background:#fff;color:#111;cursor:pointer}" +
    "button.primary{background:#111;color:#fff;border-color:#111}button.danger{color:#b91c1c;border-color:#fca5a5}" +
    "button:disabled{opacity:.5;cursor:default}" +
    "form{display:flex;gap:.5em;flex-wrap:wrap;align-items:center;margin:.5em 0}" +
    ".status{opacity:.75;font-size:.9em}.error{color:#b91c1c;font-size:.9em}.muted{opacity:.6;font-size:.85em}" +
    ".card{border:1px solid #e5e7eb;border-radius:10px;padding:.8em 1em;margin:.6em 0}" +
    ".held{color:#92400e}.row-actions{display:flex;gap:.35em;flex-wrap:wrap}" +
    "code{background:#f3f4f6;padding:.1em .3em;border-radius:.2em;font-size:.9em}" +
    ".dns td{font-family:ui-monospace,monospace;font-size:.85em}" +
    "@media(prefers-color-scheme:dark){button{background:#222;color:#eee;border-color:#444}button.primary{background:#eee;color:#111;border-color:#eee}" +
    "th,td{border-color:#333}.card{border-color:#333}input,select{background:#1a1a1a;color:#eee;border-color:#444}code{background:#222}}";

  // NetworkElement: a FriendoElement that also re-renders when the network
  // account signs in or out.
  class NetworkElement extends FriendoElement {
    constructor() {
      super();
      this._onAccount = this._onAccount.bind(this);
    }
    connectedCallback() {
      document.addEventListener("friendo:account", this._onAccount);
      super.connectedCallback();
    }
    disconnectedCallback() {
      document.removeEventListener("friendo:account", this._onAccount);
      super.disconnectedCallback();
    }
    _onAccount() { this.render(); }
    css() { return NET_CSS; }
    // paintSignIn draws the email → code form into a container. Messages from
    // the network (invite-only, suspended, too many codes) are shown as-is:
    // they already say what to do.
    paintSignIn(container, intro) {
      var self = this;
      container.innerHTML =
        (intro ? '<p part="intro" class="status">' + intro + "</p>" : "") +
        '<form part="signin"><input part="email" type="email" required placeholder="you@example.com" autocomplete="email" />' +
        '<button part="send" class="primary" type="submit">Email me a code</button></form>' +
        '<p part="status" class="status"></p>';
      var form = container.querySelector("form");
      var status = container.querySelector('[part="status"]');
      form.onsubmit = async function (e) {
        e.preventDefault();
        var email = container.querySelector('[part="email"]').value.trim();
        if (!email) return;
        status.className = "status";
        status.textContent = "Sending…";
        try {
          var r = await napi("/auth/request-code", jsonBody("POST", { email: email }));
          self.paintVerify(container, email, r && r.code, r && r.emailed === false);
        } catch (err) {
          status.className = "error";
          status.textContent = err.message;
        }
      };
    }
    paintVerify(container, email, devCode, notEmailed) {
      var self = this;
      var note = devCode
        ? "No email provider is set up on this network, so here's your code: <b>" + esc(devCode) + "</b>"
        : notEmailed
          ? "No email provider is set up — the code is in the network's server log."
          : "We sent a 6-digit code to <b>" + esc(email) + "</b>.";
      container.innerHTML =
        '<p part="status" class="status">' + note + "</p>" +
        '<form part="verify"><input part="code" inputmode="numeric" autocomplete="one-time-code" required placeholder="6-digit code" maxlength="6" />' +
        '<button part="signin-button" class="primary" type="submit">Sign in</button>' +
        '<button part="back" type="button">Different email</button></form>' +
        '<p part="error" class="error"></p>';
      var input = container.querySelector('[part="code"]');
      if (devCode) input.value = devCode;
      var error = container.querySelector('[part="error"]');
      container.querySelector('[part="back"]').onclick = function () { self.paintSignIn(container); };
      container.querySelector("form").onsubmit = async function (e) {
        e.preventDefault();
        error.textContent = "";
        try {
          var acct = await napi("/auth/verify-code", jsonBody("POST", { email: email, code: input.value.trim() }));
          broadcastAccount(acct);
        } catch (err) {
          error.textContent = err.message;
        }
      };
    }
    signedInBar(acct) {
      return (
        '<p part="account" class="status">Signed in as <b>' + esc(acct.email) + "</b>" +
        (acct.operator ? " · operator" : "") +
        ' · <button part="logout">Sign out</button></p>'
      );
    }
    wireLogout() {
      var btn = this.shadowRoot.querySelector('[part="logout"]');
      if (btn) btn.onclick = async function () {
        await napi("/auth/logout", { method: "POST" }).catch(function () {});
        broadcastAccount(null);
      };
    }
  }

  // --- <friendo-account> ---------------------------------------------------
  // Your sites on this network: open each one's admin, connect a domain, make
  // a new site — and how many you can still make.
  class FriendoAccount extends NetworkElement {
    async render() {
      var acct = await currentAccount();
      if (!acct) {
        this.paint('<div part="signin-wrap"></div>');
        this.paintSignIn(this.shadowRoot.querySelector('[part="signin-wrap"]'), "Sign in to see your sites.");
        return;
      }
      var data;
      try {
        data = await napi("/account/sites");
      } catch (err) {
        if (err.status === 401 || err.status === 403) { broadcastAccount(null); return; }
        this.paint('<p class="error">' + esc(err.message) + "</p>");
        return;
      }
      var sites = data.sites || [];
      var q = data.quota || {};
      var quotaLine = q.exempt
        ? "As an operator you can make as many sites as you like."
        : q.unlimited
          ? "You can make as many sites as you like."
          : (q.used || 0) + " of " + q.allowed + " sites used.";
      var atLimit = !q.exempt && !q.unlimited && q.used >= q.allowed;

      this.paint(
        this.signedInBar(acct) +
          '<h2 part="sites-heading">Your sites</h2>' +
          (sites.length
            ? '<table part="sites"><thead><tr><th>Site</th><th>Address</th><th></th></tr></thead><tbody>' +
              sites.map(function (s) {
                return (
                  '<tr part="site" data-sub="' + esc(s.subdomain) + '"><td><b>' + esc(s.name) + "</b>" +
                  (s.suspended ? '<br><span class="held">On hold' + (s.reason ? ": " + esc(s.reason) : "") + "</span>" : "") +
                  '</td><td><a part="address" href="' + esc(siteHref(s.address)) + '">' + esc(s.address) + "</a>" +
                  (s.domains || []).map(function (d) {
                    return '<br><span class="muted">' + esc(d.domain) + " — " + esc(d.status) + "</span>";
                  }).join("") +
                  '</td><td class="row-actions"><button part="open-admin" class="primary" data-sub="' + esc(s.subdomain) + '">Open admin</button>' +
                  '<button part="domains-toggle" data-sub="' + esc(s.subdomain) + '">Domains</button></td></tr>' +
                  '<tr part="domains-row" data-for="' + esc(s.subdomain) + '" hidden><td colspan="3"><div part="domains" class="card"></div></td></tr>'
                );
              }).join("") +
              "</tbody></table>"
            : '<p part="empty" class="status">You don\'t have any sites here yet.</p>') +
          '<p part="quota" class="muted">' + esc(quotaLine) + "</p>" +
          '<h2 part="new-heading">New site</h2>' +
          '<form part="new-site"><input part="new-subdomain" required placeholder="my-site" pattern="[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?" title="lowercase letters, digits and hyphens" />' +
          '<span class="muted">.' + esc(location.hostname) + "</span>" +
          '<input part="new-name" placeholder="Display name (optional)" />' +
          '<button part="create" class="primary" type="submit"' + (atLimit ? " disabled" : "") + ">Create site</button></form>" +
          '<p part="new-status" class="error"></p>' +
          '<p class="muted">Or from your computer: <code>friendo deploy my-site --network ' + esc(location.origin) + "</code></p>"
      );
      this.wireLogout();
      var self = this;
      var root = this.shadowRoot;

      root.querySelectorAll('[part="open-admin"]').forEach(function (btn) {
        btn.onclick = async function () {
          btn.disabled = true;
          btn.textContent = "Opening…";
          try {
            var r = await napi("/account/sites/" + encodeURIComponent(btn.dataset.sub) + "/admin-link", { method: "POST" });
            location.assign(r.url);
          } catch (err) {
            btn.disabled = false;
            btn.textContent = "Open admin";
            root.querySelector('[part="new-status"]').textContent = err.message;
          }
        };
      });
      root.querySelectorAll('[part="domains-toggle"]').forEach(function (btn) {
        btn.onclick = function () {
          var row = root.querySelector('[part="domains-row"][data-for="' + btn.dataset.sub + '"]');
          row.hidden = !row.hidden;
          if (!row.hidden) self._paintDomains(row.querySelector('[part="domains"]'), btn.dataset.sub);
        };
      });
      var form = root.querySelector('[part="new-site"]');
      form.onsubmit = async function (e) {
        e.preventDefault();
        var status = root.querySelector('[part="new-status"]');
        status.textContent = "";
        var sub = root.querySelector('[part="new-subdomain"]').value.trim().toLowerCase();
        var name = root.querySelector('[part="new-name"]').value.trim();
        try {
          await napi("/account/sites", jsonBody("POST", { subdomain: sub, name: name }));
          self.dispatchEvent(new CustomEvent("friendo:site-created", { bubbles: true, detail: { subdomain: sub } }));
          self.render();
        } catch (err) {
          status.textContent = err.message;
        }
      };
    }
    // _paintDomains: the domains on one site, with add / verify / remove.
    async _paintDomains(box, sub) {
      var self = this;
      box.innerHTML = '<p class="status">Loading…</p>';
      var list = [];
      try {
        list = (await napi("/account/domains?site=" + encodeURIComponent(sub))).domains || [];
      } catch (err) {
        box.innerHTML = '<p class="error">' + esc(err.message) + "</p>";
        return;
      }
      box.innerHTML =
        "<h3>Your own domain for " + esc(sub) + "</h3>" +
        (list.length
          ? '<table part="domain-list">' +
            list.map(function (d) {
              return (
                '<tr part="domain" data-domain="' + esc(d.domain) + '"><td>' + esc(d.domain) + "</td><td>" + esc(d.status) + "</td><td class=\"row-actions\">" +
                (d.verified ? "" : '<button part="verify" data-domain="' + esc(d.domain) + '">Check &amp; go live</button>') +
                '<button part="remove-domain" class="danger" data-domain="' + esc(d.domain) + '">Disconnect</button></td></tr>'
              );
            }).join("") +
            "</table>"
          : '<p class="muted">No domain connected. Your site is at its network address.</p>') +
        '<form part="add-domain"><input part="domain-input" required placeholder="example.com" />' +
        '<button part="add-domain-button" class="primary" type="submit">Connect</button></form>' +
        '<div part="dns"></div><p part="domain-status" class="error"></p>';
      var status = box.querySelector('[part="domain-status"]');
      var dns = box.querySelector('[part="dns"]');
      box.querySelector("form").onsubmit = async function (e) {
        e.preventDefault();
        status.textContent = "";
        var domain = box.querySelector('[part="domain-input"]').value.trim();
        try {
          var r = await napi("/account/domains", jsonBody("POST", { domain: domain, subdomain: sub }));
          self._paintInstructions(dns, r);
        } catch (err) {
          status.textContent = err.message;
        }
      };
      box.querySelectorAll('[part="verify"]').forEach(function (btn) {
        btn.onclick = async function () {
          status.className = "status";
          status.textContent = "Checking…";
          try {
            await napi("/account/domains/verify", jsonBody("POST", { domain: btn.dataset.domain }));
            self._paintDomains(box, sub);
          } catch (err) {
            status.className = "error";
            status.textContent = err.message;
          }
        };
      });
      box.querySelectorAll('[part="remove-domain"]').forEach(function (btn) {
        btn.onclick = async function () {
          if (!confirmed("Disconnect " + btn.dataset.domain + "? The site keeps its network address.")) return;
          try {
            await napi("/account/domains", jsonBody("DELETE", { domain: btn.dataset.domain }));
            self._paintDomains(box, sub);
          } catch (err) {
            status.textContent = err.message;
          }
        };
      });
    }
    _paintInstructions(dns, r) {
      var recs = (r.instructions && r.instructions.records) || [];
      dns.innerHTML =
        '<div class="card"><b>' + esc(r.domain) + "</b> is connected but not live yet. Add these records with whoever you bought the domain from, then press <i>Check &amp; go live</i>:" +
        '<table class="dns" part="dns-table"><tr><th>Type</th><th>Name</th><th>Value</th></tr>' +
        recs.map(function (rec) {
          return "<tr><td>" + esc(rec.type) + "</td><td>" + esc(rec.name) + "</td><td>" + esc(rec.value) + "</td></tr>" +
            (rec.why ? '<tr><td></td><td colspan="2" class="muted">' + esc(rec.why) + "</td></tr>" : "");
        }).join("") +
        "</table>" + (r.instructions && r.instructions.note ? '<p class="muted">' + esc(r.instructions.note) + "</p>" : "") + "</div>";
    }
  }

  // --- <friendo-console> ---------------------------------------------------
  // The operator's levers: sites, who can join, limits, people, invites,
  // domains, and which site the bare domain shows.
  class FriendoConsole extends NetworkElement {
    async render() {
      var acct = await currentAccount();
      if (!acct) {
        this.paint('<div part="signin-wrap"></div>');
        this.paintSignIn(this.shadowRoot.querySelector('[part="signin-wrap"]'), "Sign in to run this network.");
        return;
      }
      if (!acct.operator) {
        this.paint(
          this.signedInBar(acct) +
            '<p part="not-operator" class="status">Your account isn\'t an operator of this network. ' +
            '<a href="/account">See your sites instead.</a></p>'
        );
        this.wireLogout();
        return;
      }
      var net, sites, people, invites, domains;
      try {
        var all = await Promise.all([
          napi("/network"), napi("/network/sites"), napi("/network/accounts"), napi("/network/invites"), napi("/network/domains"),
        ]);
        net = all[0]; sites = all[1].sites || []; people = all[2].accounts || []; invites = all[3].invites || []; domains = all[4].domains || [];
      } catch (err) {
        if (err.status === 401) { broadcastAccount(null); return; }
        this.paint('<p class="error">' + esc(err.message) + "</p>");
        return;
      }
      var self = this;
      var counts = net.counts || {};
      this.paint(
        this.signedInBar(acct) +
          '<p part="summary" class="muted">' + esc(net.base) + " · " + (counts.sites || 0) + " site(s) · " + (counts.accounts || 0) + " account(s)</p>" +
          '<p part="error" class="error"></p>' +

          '<h2 part="home-heading">Home site</h2>' +
          '<p class="muted">What the bare domain (' + esc(net.base) + ") shows. A home site can take over /account, /login or /network by defining that page itself.</p>" +
          '<form part="home-site"><select part="home-select">' +
          '<option value=""' + (net.home_site ? "" : " selected") + ">Built-in landing page</option>" +
          sites.map(function (s) {
            return '<option value="' + esc(s.subdomain) + '"' + (net.home_site === s.subdomain ? " selected" : "") + ">" + esc(s.subdomain) + " — " + esc(s.name) + "</option>";
          }).join("") +
          '</select><button part="home-save" class="primary" type="submit">Save</button></form>' +

          '<h2 part="sites-heading">Sites</h2>' +
          '<form part="create-site"><input part="create-subdomain" required placeholder="subdomain" />' +
          '<input part="create-name" placeholder="Display name" /><input part="create-owner" type="email" placeholder="Owner email (defaults to you)" />' +
          '<button part="create" class="primary" type="submit">Create</button></form>' +
          (sites.length
            ? '<table part="sites"><thead><tr><th>Site</th><th>Owner</th><th>Status</th><th></th></tr></thead><tbody>' +
              sites.map(function (s) {
                return (
                  '<tr part="site" data-sub="' + esc(s.subdomain) + '"><td><a href="' + esc(siteHref(s.address)) + '">' + esc(s.subdomain) + "</a><br><span class=\"muted\">" + esc(s.name) + "</span>" +
                  (s.domains || []).map(function (d) { return '<br><span class="muted">' + esc(d.domain) + " — " + esc(d.status) + "</span>"; }).join("") +
                  "</td><td>" + (s.owner ? esc(s.owner) : '<span class="muted">—</span>') + "</td>" +
                  "<td>" + (s.suspended ? '<span class="held">On hold' + (s.reason ? ": " + esc(s.reason) : "") + "</span>" : "Live") + "</td>" +
                  '<td class="row-actions">' +
                  (s.suspended
                    ? '<button data-act="site-resume" data-sub="' + esc(s.subdomain) + '">Put back</button>'
                    : '<button data-act="site-suspend" data-sub="' + esc(s.subdomain) + '">Hold</button>') +
                  '<button data-act="site-owner" data-sub="' + esc(s.subdomain) + '">Owner</button>' +
                  '<button class="danger" data-act="site-destroy" data-sub="' + esc(s.subdomain) + '">Delete</button></td></tr>'
                );
              }).join("") +
              "</tbody></table>"
            : '<p class="muted">No sites yet.</p>') +

          '<h2 part="join-heading">Who can join</h2>' +
          '<form part="signups"><label><input type="radio" name="signups" value="invite"' + (net.signups === "invite" ? " checked" : "") + "> Invite-only</label>" +
          '<label><input type="radio" name="signups" value="open"' + (net.signups === "open" ? " checked" : "") + "> Anyone with an email</label></form>" +

          '<h2 part="limit-heading">Site limit</h2>' +
          '<form part="quota"><label>Each account can make <input part="quota-input" size="8" value="' + esc(net.default_quota) + '" /> site(s)</label>' +
          '<button part="quota-save" type="submit">Save</button><span class="muted">a number, or "unlimited"</span></form>' +

          '<h2 part="people-heading">People</h2>' +
          (people.length
            ? '<table part="people"><thead><tr><th>Email</th><th>Role</th><th>Sites</th><th>Status</th><th></th></tr></thead><tbody>' +
              people.map(function (p) {
                var limit = p.unlimited ? "unlimited" : p.allowed;
                return (
                  '<tr part="person" data-id="' + esc(p.id) + '"><td>' + esc(p.email) + "</td><td>" + (p.operator ? "operator" : "member") + "</td>" +
                  "<td>" + p.used + (p.operator ? "" : " of " + limit + (p.override ? ' <span class="muted">(own limit)</span>' : "")) + "</td>" +
                  "<td>" + (p.suspended ? '<span class="held">Suspended' + (p.reason ? ": " + esc(p.reason) : "") + "</span>" : "Active") + "</td>" +
                  '<td class="row-actions">' +
                  (p.operator
                    ? (p.email === acct.email ? "" : '<button data-act="op-revoke" data-email="' + esc(p.email) + '">Remove operator</button>')
                    : '<button data-act="op-grant" data-email="' + esc(p.email) + '">Make operator</button>' +
                      '<button data-act="limit" data-id="' + esc(p.id) + '">Limit</button>' +
                      (p.suspended
                        ? '<button data-act="acct-resume" data-id="' + esc(p.id) + '">Let back in</button>'
                        : '<button data-act="acct-suspend" data-id="' + esc(p.id) + '">Suspend</button>')) +
                  '<button data-act="signout" data-id="' + esc(p.id) + '">Sign out everywhere</button></td></tr>'
                );
              }).join("") +
              "</tbody></table>"
            : '<p class="muted">No accounts yet.</p>') +

          '<h2 part="invites-heading">Invites</h2>' +
          '<form part="invite"><input part="invite-email" type="email" required placeholder="someone@example.com" />' +
          '<label>good for <input part="invite-days" type="number" min="1" value="14" size="4" /> days</label>' +
          '<label><input part="invite-operator" type="checkbox" /> as an operator</label>' +
          '<button part="invite-send" class="primary" type="submit">Invite</button></form>' +
          (invites.length
            ? '<table part="invites">' + invites.map(function (i) {
                return '<tr><td>' + esc(i.email) + "</td><td>" + esc(i.status) + "</td><td>" + esc(i.expires) + '</td><td><button data-act="invite-revoke" data-email="' + esc(i.email) + '">Revoke</button></td></tr>';
              }).join("") + "</table>" +
              '<p><button data-act="invite-prune">Forget expired invites</button></p>'
            : '<p class="muted">No invites outstanding.</p>') +

          '<h2 part="domains-heading">Custom domains</h2>' +
          '<form part="add-domain"><input part="domain-input" required placeholder="example.com" />' +
          '<select part="domain-site">' + sites.map(function (s) { return '<option value="' + esc(s.subdomain) + '">' + esc(s.subdomain) + "</option>"; }).join("") + "</select>" +
          '<button part="domain-add" class="primary" type="submit">Connect</button></form><div part="dns"></div>' +
          (domains.length
            ? '<table part="domains">' + domains.map(function (d) {
                return '<tr><td>' + esc(d.domain) + "</td><td>" + esc(d.site) + "</td><td>" + esc(d.status) + '</td><td class="row-actions">' +
                  (d.verified ? "" : '<button data-act="domain-verify" data-domain="' + esc(d.domain) + '">Check</button>') +
                  '<button class="danger" data-act="domain-remove" data-domain="' + esc(d.domain) + '">Disconnect</button></td></tr>';
              }).join("") + "</table>"
            : '<p class="muted">No custom domains connected. Tenants connect their own from their account page or with <code>friendo domain add</code>.</p>')
      );
      this.wireLogout();
      var root = this.shadowRoot;
      var error = root.querySelector('[part="error"]');
      var act = async function (fn) {
        error.textContent = "";
        try { await fn(); self.render(); } catch (err) { error.textContent = err.message; }
      };
      var settings = function (patch) { return napi("/network/settings", jsonBody("PUT", patch)); };

      root.querySelector('[part="home-site"]').onsubmit = function (e) {
        e.preventDefault();
        act(function () { return settings({ home_site: root.querySelector('[part="home-select"]').value }); });
      };
      root.querySelector('[part="create-site"]').onsubmit = function (e) {
        e.preventDefault();
        act(function () {
          return napi("/sites", jsonBody("POST", {
            subdomain: root.querySelector('[part="create-subdomain"]').value.trim().toLowerCase(),
            name: root.querySelector('[part="create-name"]').value.trim(),
            owner: root.querySelector('[part="create-owner"]').value.trim(),
          }));
        });
      };
      root.querySelectorAll('[part="signups"] input').forEach(function (radio) {
        radio.onchange = function () { act(function () { return settings({ signups: radio.value }); }); };
      });
      root.querySelector('[part="quota"]').onsubmit = function (e) {
        e.preventDefault();
        act(function () { return settings({ default_quota: root.querySelector('[part="quota-input"]').value.trim() }); });
      };
      root.querySelector('[part="invite"]').onsubmit = function (e) {
        e.preventDefault();
        act(function () {
          return napi("/network/invites", jsonBody("POST", {
            email: root.querySelector('[part="invite-email"]').value.trim(),
            days: parseInt(root.querySelector('[part="invite-days"]').value, 10) || 0,
            operator: root.querySelector('[part="invite-operator"]').checked,
          }));
        });
      };
      root.querySelector('[part="add-domain"]').onsubmit = function (e) {
        e.preventDefault();
        error.textContent = "";
        napi("/network/domains", jsonBody("POST", {
          domain: root.querySelector('[part="domain-input"]').value.trim(),
          site: root.querySelector('[part="domain-site"]').value,
        })).then(function (r) {
          FriendoAccount.prototype._paintInstructions.call(self, root.querySelector('[part="dns"]'), r);
        }, function (err) { error.textContent = err.message; });
      };

      root.querySelectorAll("[data-act]").forEach(function (btn) {
        var d = btn.dataset;
        btn.onclick = function () {
          switch (d.act) {
            case "site-suspend": {
              var why = window.prompt("Put " + d.sub + " on hold. Reason (shown on the hold page, optional):", "");
              if (why === null) return;
              return act(function () { return napi("/network/sites/" + d.sub + "/suspend", jsonBody("POST", { reason: why })); });
            }
            case "site-resume": return act(function () { return napi("/network/sites/" + d.sub + "/resume", { method: "POST" }); });
            case "site-owner": {
              var who = window.prompt("Email of the account that should own " + d.sub + ":", "");
              if (!who) return;
              return act(function () { return napi("/network/sites/" + d.sub + "/owner", jsonBody("PUT", { email: who })); });
            }
            case "site-destroy":
              if (!confirmed("Permanently delete " + d.sub + " and all its data? This can't be undone.")) return;
              return act(function () { return napi("/sites/" + d.sub, { method: "DELETE" }); });
            case "op-grant": return act(function () { return napi("/network/operators", jsonBody("POST", { email: d.email })); });
            case "op-revoke": return act(function () { return napi("/network/operators/" + encodeURIComponent(d.email), { method: "DELETE" }); });
            case "limit": {
              var n = window.prompt("How many sites may this account make? A number, \"unlimited\", or \"default\":", "default");
              if (n === null) return;
              return act(function () { return napi("/network/accounts/" + d.id + "/quota", jsonBody("PUT", { sites: n })); });
            }
            case "acct-suspend": {
              var reason = window.prompt("Suspend this account. Reason (shown to them when they try to sign in, optional):", "");
              if (reason === null) return;
              return act(function () { return napi("/network/accounts/" + d.id + "/suspend", jsonBody("POST", { reason: reason })); });
            }
            case "acct-resume": return act(function () { return napi("/network/accounts/" + d.id + "/resume", { method: "POST" }); });
            case "signout": return act(function () { return napi("/network/accounts/" + d.id + "/signout", { method: "POST" }); });
            case "invite-revoke": return act(function () { return napi("/network/invites/" + encodeURIComponent(d.email), { method: "DELETE" }); });
            case "invite-prune": return act(function () { return napi("/network/invites/prune", { method: "POST" }); });
            case "domain-verify": return act(function () { return napi("/network/domains/verify", jsonBody("POST", { domain: d.domain })); });
            case "domain-remove":
              if (!confirmed("Disconnect " + d.domain + "?")) return;
              return act(function () { return napi("/network/domains", jsonBody("DELETE", { domain: d.domain })); });
          }
        };
      });
    }
  }

  // --- <friendo-activate code="K7QP-2XR9"> ---------------------------------
  // The browser half of `friendo login`: sign in (if needed), then approve the
  // code the terminal showed.
  class FriendoActivate extends NetworkElement {
    async render() {
      var acct = await currentAccount();
      var code = (this.getAttribute("code") || "").toUpperCase();
      if (!acct) {
        this.paint('<div part="signin-wrap"></div>');
        this.paintSignIn(
          this.shadowRoot.querySelector('[part="signin-wrap"]'),
          "Sign in to link the device that's waiting in your terminal" + (code ? " (code <b>" + esc(code) + "</b>)" : "") + "."
        );
        return;
      }
      this.paint(
        this.signedInBar(acct) +
          '<form part="approve"><label>Code from your terminal <input part="code" required value="' + esc(code) + '" placeholder="K7QP-2XR9" /></label>' +
          '<button part="approve-button" class="primary" type="submit">Link this device</button></form>' +
          '<p part="status" class="status"></p>'
      );
      this.wireLogout();
      var root = this.shadowRoot;
      var status = root.querySelector('[part="status"]');
      root.querySelector("form").onsubmit = async function (e) {
        e.preventDefault();
        status.className = "status";
        status.textContent = "Linking…";
        try {
          await napi("/auth/device/approve", jsonBody("POST", { user_code: root.querySelector('[part="code"]').value.trim().toUpperCase() }));
          root.querySelector("form").hidden = true;
          status.innerHTML = '<span part="done">Device linked as <b>' + esc(acct.email) + "</b>. You can close this tab and go back to your terminal.</span>";
        } catch (err) {
          status.className = "error";
          status.textContent = err.message;
        }
      };
    }
  }

  var defs = {
    "friendo-auth": FriendoAuth,
    "friendo-comments": FriendoComments,
    "friendo-reactions": FriendoReactions,
    "friendo-poll": FriendoPoll,
    "friendo-channel": FriendoChannel,
    "friendo-map": FriendoMap,
    "friendo-input": FriendoInput,
    "friendo-form": FriendoForm,
    "friendo-calendar": FriendoCalendar,
    "friendo-rsvp": FriendoRSVP,
    "friendo-add-to-calendar": FriendoAddToCalendar,
    "friendo-account": FriendoAccount,
    "friendo-console": FriendoConsole,
    "friendo-activate": FriendoActivate,
  };
  Object.keys(defs).forEach(function (tag) {
    if (!customElements.get(tag)) customElements.define(tag, defs[tag]);
  });
})();
