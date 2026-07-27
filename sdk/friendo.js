/**
 * friendo.js — the community SDK.
 *
 * Drop-in Web Components that add member-gated comments, reactions, and polls to
 * any Friendo site with no custom JavaScript:
 *
 *   <script src="/friendo.js" defer></script>
 *   <friendo-auth></friendo-auth>
 *   <friendo-comments post-id="…"></friendo-comments>
 *   <friendo-reactions target-type="post" target-id="…"></friendo-reactions>
 *   <friendo-poll poll-id="…"></friendo-poll>
 *   <friendo-map target-type="post" target-id="…"></friendo-map>
 *   <friendo-form collection="posts">…author inputs…</friendo-form>
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

  // Shared JSON fetch. Cookies ride along (the friendo_session cookie's path is
  // /_/, which matches every /_/api request).
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
    css() {
      return "";
    }
    render() {}
  }

  // --- <friendo-auth> ------------------------------------------------------
  // Passwordless email + one-time-code login. Emits friendo:auth on state change.
  class FriendoAuth extends FriendoElement {
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
        broadcastAuth(null);
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
          broadcastAuth(r.user);
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
        this.paint('<div part="error">' + esc(e.message) + "</div>");
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
        this.paint('<div part="error">' + esc(e.message) + "</div>");
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
        this.paint('<div part="error">' + esc(e.message) + "</div>");
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
        this.paint('<div part="error">' + esc(e.message) + "</div>");
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
        this.paint('<div part="error">' + esc(e.message) + "</div>");
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
        this.paint('<div part="error">' + esc(e.message) + "</div>");
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
        ".chip button{border:0;background:none;font:inherit;cursor:pointer;opacity:.7;padding:0;line-height:1}"
      );
    }
    get type() {
      return (this.getAttribute("type") || "text").toLowerCase();
    }
    // Read by <friendo-form> at submit time. Shape depends on `type`.
    get value() {
      switch (this.type) {
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
        this.paint('<div part="error">' + esc(e.message) + "</div>");
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
          if ((el.getAttribute("type") || "").toLowerCase() === "media") {
            media.push({ name: name, el: el });
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
          record.data = record.data || {};
          record.data[media[i].name] = up.file.url;
          patched = true;
        } catch (err) {
          /* the post exists; the asset just didn't attach */
        }
      }
      if (patched) {
        try {
          await api("/records/" + encodeURIComponent(record.id), jsonBody("PUT", {
            title: record.title, body: record.body, slug: record.slug, status: record.status, data: record.data,
          }));
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

  var defs = {
    "friendo-auth": FriendoAuth,
    "friendo-comments": FriendoComments,
    "friendo-reactions": FriendoReactions,
    "friendo-poll": FriendoPoll,
    "friendo-channel": FriendoChannel,
    "friendo-map": FriendoMap,
    "friendo-input": FriendoInput,
    "friendo-form": FriendoForm,
  };
  Object.keys(defs).forEach(function (tag) {
    if (!customElements.get(tag)) customElements.define(tag, defs[tag]);
  });
})();
