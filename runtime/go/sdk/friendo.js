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

  var defs = {
    "friendo-auth": FriendoAuth,
    "friendo-comments": FriendoComments,
    "friendo-reactions": FriendoReactions,
    "friendo-poll": FriendoPoll,
    "friendo-channel": FriendoChannel,
    "friendo-map": FriendoMap,
  };
  Object.keys(defs).forEach(function (tag) {
    if (!customElements.get(tag)) customElements.define(tag, defs[tag]);
  });
})();
