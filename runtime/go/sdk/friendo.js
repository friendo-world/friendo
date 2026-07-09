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
        ".status{opacity:.7;font-size:.9em}"
      );
    }
    async render() {
      var user = await currentUser();
      if (user) {
        this.paint(
          '<div part="signed-in" class="status">Signed in as <b part="name">' +
            esc(user.name || user.email) +
            '</b> · <button part="logout">Sign out</button></div>'
        );
        this.shadowRoot.querySelector('[part="logout"]').onclick = async () => {
          await api("/auth/logout", { method: "POST" }).catch(function () {});
          broadcastAuth(null);
        };
        return;
      }
      this._renderRequest();
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

  var defs = {
    "friendo-auth": FriendoAuth,
    "friendo-comments": FriendoComments,
    "friendo-reactions": FriendoReactions,
    "friendo-poll": FriendoPoll,
  };
  Object.keys(defs).forEach(function (tag) {
    if (!customElements.get(tag)) customElements.define(tag, defs[tag]);
  });
})();
