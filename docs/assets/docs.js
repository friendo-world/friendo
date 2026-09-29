// docs.friendo.world — everything the docs add on top of the page.
//
// One plain script, no dependencies. Each part is its own function guarded by
// what it needs, so a missing feature (no clipboard, no dialog) removes that
// part and nothing else. Order matters where parts touch the same elements:
// copy wraps every <pre>, tabs moves those wrappers into panels, rail moves
// panels and wrappers into columns, and toc/search read the final headings.

(function () {
  "use strict";
  var root = document.documentElement;
  var doc = document.querySelector("article.doc");
  var body = document.querySelector(".doc-body");

  // ---- theme -------------------------------------------------------------
  // The footer toggle flips the page and saves it. Flipping back to what the
  // system already prefers clears the saved choice, so the page follows the
  // system again. The button always names the mode it would switch to.
  function theme() {
    var btn = document.querySelector(".theme-toggle");
    if (!btn) return;
    var system = window.matchMedia("(prefers-color-scheme: dark)");
    function current() { return root.getAttribute("data-theme") || (system.matches ? "dark" : "light"); }
    function relabel() { btn.textContent = current() === "dark" ? "light" : "dark"; }
    btn.addEventListener("click", function () {
      var next = current() === "dark" ? "light" : "dark";
      var systemWants = system.matches ? "dark" : "light";
      try {
        if (next === systemWants) { localStorage.removeItem("theme"); root.removeAttribute("data-theme"); }
        else { localStorage.setItem("theme", next); root.setAttribute("data-theme", next); }
      } catch (e) { root.setAttribute("data-theme", next); }
      relabel();
    });
    system.addEventListener("change", relabel);
    relabel();
  }

  // ---- nav: the active link, and prev / next ------------------------------
  // The sidebar is already in reading order (section, then weight), so the
  // pager is just the neighbours of the active link.
  function nav() {
    var slug = location.pathname.replace(/^\/docs\//, "").replace(/\/$/, "");
    var links = Array.prototype.slice.call(document.querySelectorAll(".sidebar nav a[data-slug]"));
    var i = links.findIndex(function (a) { return a.dataset.slug === slug; });
    if (i < 0) return;
    links[i].classList.add("active");
    var pager = document.querySelector(".pager");
    if (!pager) return;
    function link(a, cls, label) {
      var el = document.createElement("a");
      el.href = a.getAttribute("href"); el.className = cls;
      el.innerHTML = '<span class="label">' + label + "</span>" + a.textContent;
      return el;
    }
    if (i > 0) pager.appendChild(link(links[i - 1], "prev", "← Previous"));
    if (i < links.length - 1) pager.appendChild(link(links[i + 1], "next", "Next →"));
  }

  // ---- copy: a button on every code block ---------------------------------
  function copy() {
    if (!navigator.clipboard) return;
    document.querySelectorAll("main pre").forEach(function (pre) {
      var wrap = document.createElement("div");
      wrap.className = "codeblock";
      pre.parentNode.insertBefore(wrap, pre);
      wrap.appendChild(pre);
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "copy-code label";
      btn.textContent = "✿ copy";
      btn.setAttribute("aria-label", "Copy code");
      btn.addEventListener("click", function () {
        navigator.clipboard.writeText(pre.textContent.replace(/\n$/, "")).then(function () {
          btn.textContent = "✿ copied";
          btn.classList.add("done");
          setTimeout(function () { btn.textContent = "✿ copy"; btn.classList.remove("done"); }, 1500);
        });
      });
      wrap.appendChild(btn);
    });
  }

  // ---- callouts: a blockquote with a bold lead ----------------------------
  // > **Tip:** … becomes a callout of kind "tip". Four kinds; anything else
  // stays an ordinary note.
  var KINDS = { note: "Note", tip: "Tip", warning: "Warning", "common-mistake": "Common mistake" };
  function callouts() {
    if (!body) return;
    body.querySelectorAll("blockquote").forEach(function (q) {
      var p = q.firstElementChild;
      if (!p || p.tagName !== "P") return;
      var s = p.firstElementChild;
      if (!s || s.tagName !== "STRONG" || s !== p.firstChild) return;
      var text = s.textContent.trim().replace(/[:.]$/, "");
      var kind = Object.keys(KINDS).find(function (k) { return KINDS[k].toLowerCase() === text.toLowerCase(); });
      if (!kind) return;
      q.classList.add("callout", "callout-" + kind);
      s.classList.add("callout-kind", "label");
      s.textContent = KINDS[kind];
      // Drop the ":" that followed the lead, and any leading space.
      var next = s.nextSibling;
      if (next && next.nodeType === 3) next.textContent = next.textContent.replace(/^[:.]?\s*/, "");
    });
  }

  // ---- highlight: a small tokenizer per language --------------------------
  // Works on the text, never on markup: the code is split into tokens, each
  // becomes a text node or a <span class="tok-…">. Three roles: comment,
  // string, keyword (plus attr); diff adds add/del.
  var RULES = {
    bash: [
      [/#.*$/m, "comment"],
      [/"(?:[^"\\]|\\.)*"|'[^']*'/, "string"],
      [/^\s*(?:\$ )?(friendo|curl|cd|git|go|npm|sh|export|mkdir|docker|ls|cat|echo|open|cp|mv|rm|ssh|scp)\b/m, "keyword"],
      [/(?:^|\s)(--?[a-z][\w-]*)/, "attr"],
    ],
    toml: [
      [/#.*$/m, "comment"],
      [/^\s*\[[^\]]+\]\s*$/m, "keyword"],
      [/"(?:[^"\\]|\\.)*"|'[^']*'/, "string"],
      [/^\s*[\w.-]+(?=\s*=)/m, "attr"],
    ],
    yaml: [
      [/#.*$/m, "comment"],
      [/^\s*---\s*$/m, "comment"],
      [/^\s*-?\s*[\w.-]+(?=:(?:\s|$))/m, "attr"],
      [/"(?:[^"\\]|\\.)*"|'[^']*'/, "string"],
    ],
    html: [
      [/<!--[\s\S]*?-->|\{#[\s\S]*?#\}/, "comment"],
      [/\{\{[\s\S]*?\}\}|\{%[\s\S]*?%\}/, "keyword"],
      [/"[^"]*"|'[^']*'/, "string"],
      [/<\/?[a-zA-Z][\w:-]*|\/?>/, "attr"],
    ],
    css: [
      [/\/\*[\s\S]*?\*\//, "comment"],
      [/"[^"]*"|'[^']*'/, "string"],
      [/[^{}\n;]+(?=\s*\{)/, "keyword"],
      [/[\w-]+(?=\s*:)/, "attr"],
    ],
    json: [
      [/"(?:[^"\\]|\\.)*"(?=\s*:)/, "attr"],
      [/"(?:[^"\\]|\\.)*"/, "string"],
      [/\b(?:true|false|null)\b/, "keyword"],
    ],
    diff: [
      [/^@@.*$/m, "comment"],
      [/^\+.*$/m, "add"],
      [/^-.*$/m, "del"],
    ],
  };
  RULES.sh = RULES.bash; RULES.shell = RULES.bash; RULES.jinja = RULES.html; RULES.xml = RULES.html; RULES.yml = RULES.yaml;
  function highlight() {
    document.querySelectorAll("main pre > code[class*=language-]").forEach(function (code) {
      var lang = (code.className.match(/language-([\w-]+)/) || [])[1];
      var rules = RULES[lang];
      if (!rules) return;
      var src = code.textContent;
      var frag = document.createDocumentFragment();
      var pos = 0;
      while (pos < src.length) {
        var best = null;
        for (var r = 0; r < rules.length; r++) {
          var re = new RegExp(rules[r][0].source, "g" + rules[r][0].flags.replace("g", ""));
          re.lastIndex = pos;
          var m = re.exec(src);
          if (!m) continue;
          // For rules with a capture group, the token is the group; otherwise the whole match.
          var start = m.index, text = m[0];
          if (m.length > 1 && m[1] !== undefined) { start = m.index + m[0].indexOf(m[1]); text = m[1]; }
          if (!text) continue;
          if (!best || start < best.start) best = { start: start, text: text, cls: rules[r][1] };
        }
        if (!best) break;
        if (best.start > pos) frag.appendChild(document.createTextNode(src.slice(pos, best.start)));
        var span = document.createElement("span");
        span.className = "tok-" + best.cls;
        span.textContent = best.text;
        frag.appendChild(span);
        pos = best.start + best.text.length;
      }
      if (pos < src.length) frag.appendChild(document.createTextNode(src.slice(pos)));
      code.textContent = "";
      code.appendChild(frag);
    });
  }

  // ---- tabs: consecutive fences with tab="…" become one group -------------
  // The fence's info string arrives as data-info on the <pre>:
  //   ```bash tab="macOS / Linux" group=install
  // The choice is kept in localStorage under tabs:<group>, or under the joined
  // labels when there's no group, so the same group on another page opens on
  // the same tab. A storage event keeps other open windows in step.
  function parseInfo(info) {
    var out = {};
    var re = /(\w+)=(?:"([^"]*)"|(\S+))/g, m;
    while ((m = re.exec(info))) out[m[1]] = m[2] !== undefined ? m[2] : m[3];
    return out;
  }
  function tabs() {
    var pres = Array.prototype.slice.call(document.querySelectorAll("main pre[data-info]"));
    var groups = [], cur = null;
    pres.forEach(function (pre) {
      var info = parseInfo(pre.dataset.info);
      if (!info.tab) return;
      var block = pre.closest(".codeblock") || pre;
      var prev = block.previousElementSibling;
      var last = cur && cur.blocks[cur.blocks.length - 1];
      if (cur && prev === last) cur.blocks.push(block), cur.infos.push(info);
      else { cur = { blocks: [block], infos: [info] }; groups.push(cur); }
    });
    var byKey = {};
    groups.forEach(function (g) {
      var key = g.infos[0].group || g.infos.map(function (i) { return i.tab; }).join("|");
      var wrap = document.createElement("div");
      wrap.className = "tabs"; wrap.dataset.key = key;
      var list = document.createElement("div");
      list.className = "tablist"; list.setAttribute("role", "tablist");
      wrap.appendChild(list);
      g.blocks[0].parentNode.insertBefore(wrap, g.blocks[0]);
      g.blocks.forEach(function (block, i) {
        var label = g.infos[i].tab;
        var btn = document.createElement("button");
        btn.type = "button"; btn.setAttribute("role", "tab"); btn.textContent = label; btn.dataset.tab = label;
        list.appendChild(btn);
        var panel = document.createElement("div");
        panel.className = "panel"; panel.setAttribute("role", "tabpanel"); panel.dataset.tab = label;
        panel.appendChild(block);
        wrap.appendChild(panel);
        btn.addEventListener("click", function () { choose(key, label, true); });
        btn.addEventListener("keydown", function (e) {
          var btns = Array.prototype.slice.call(list.children), j = btns.indexOf(btn);
          if (e.key === "ArrowRight") btns[(j + 1) % btns.length].focus();
          if (e.key === "ArrowLeft") btns[(j - 1 + btns.length) % btns.length].focus();
        });
      });
      (byKey[key] = byKey[key] || []).push(wrap);
      var saved = null;
      try { saved = localStorage.getItem("tabs:" + key); } catch (e) {}
      show(wrap, saved && wrap.querySelector('.panel[data-tab="' + CSS.escape(saved) + '"]') ? saved : g.infos[0].tab);
    });
    function show(wrap, label) {
      wrap.querySelectorAll("[role=tab]").forEach(function (b) {
        var on = b.dataset.tab === label;
        b.setAttribute("aria-selected", on ? "true" : "false");
        b.tabIndex = on ? 0 : -1;
      });
      wrap.querySelectorAll(".panel").forEach(function (p) { p.hidden = p.dataset.tab !== label; });
    }
    function choose(key, label, save) {
      (byKey[key] || []).forEach(function (w) {
        if (w.querySelector('.panel[data-tab="' + CSS.escape(label) + '"]')) show(w, label);
      });
      if (save) try { localStorage.setItem("tabs:" + key, label); } catch (e) {}
    }
    window.addEventListener("storage", function (e) {
      if (e.key && e.key.indexOf("tabs:") === 0 && e.newValue) choose(e.key.slice(5), e.newValue, false);
    });
  }

  // ---- rail: reference pages read prose left, code right ------------------
  // Each run of content under a heading becomes a row; its code blocks (and
  // tab groups) move to the right column. Rows with no code stay as they are.
  // Only on wide screens, and only on Reference pages.
  function rail() {
    if (!doc || !body || doc.dataset.section !== "Reference") return;
    if (!window.matchMedia("(min-width: 1100px)").matches) return;
    var isCode = function (el) { return el.classList.contains("codeblock") || el.classList.contains("tabs") || el.tagName === "PRE"; };
    var runs = [], run = [];
    Array.prototype.slice.call(body.children).forEach(function (el) {
      if (/^H[23]$/.test(el.tagName)) { if (run.length) runs.push(run); run = []; runs.push([el]); }
      else run.push(el);
    });
    if (run.length) runs.push(run);
    var any = false;
    runs.forEach(function (r) {
      if (r.length === 1 && /^H[23]$/.test(r[0].tagName)) return;
      if (!r.some(isCode)) return;
      any = true;
      var row = document.createElement("div"); row.className = "ref-row";
      var prose = document.createElement("div"); prose.className = "ref-prose";
      var code = document.createElement("div"); code.className = "ref-code";
      r[0].parentNode.insertBefore(row, r[0]);
      r.forEach(function (el) { (isCode(el) ? code : prose).appendChild(el); });
      row.appendChild(prose); row.appendChild(code);
    });
    if (any) doc.classList.add("railed");
  }

  // ---- anchors and the "On this page" rail --------------------------------
  function toc() {
    if (!body) return;
    var heads = Array.prototype.slice.call(body.querySelectorAll("h2, h3")).filter(function (h) { return h.id; });
    heads.forEach(function (h) {
      var a = document.createElement("a");
      a.href = "#" + h.id; a.className = "anchor"; a.textContent = "✿"; a.setAttribute("aria-label", "Link to this section");
      h.appendChild(a);
    });
    var aside = document.querySelector(".toc");
    if (!aside || heads.length < 2) return;
    var title = document.createElement("p"); title.className = "label"; title.textContent = "On this page";
    var ol = document.createElement("ol");
    var links = {};
    heads.forEach(function (h) {
      var li = document.createElement("li"); li.className = h.tagName.toLowerCase();
      var a = document.createElement("a"); a.href = "#" + h.id; a.textContent = h.firstChild.textContent || h.textContent;
      a.textContent = h.textContent.replace(/✿$/, "").trim();
      li.appendChild(a); ol.appendChild(li); links[h.id] = a;
    });
    aside.appendChild(title); aside.appendChild(ol);
    // The active entry is the last heading above the fold, recomputed on
    // scroll. Deterministic, and right after a jump to an anchor too.
    var top = 0;
    function update() {
      var y = window.scrollY + 90;
      var cur = heads[0];
      for (var i = 0; i < heads.length; i++) { if (heads[i].getBoundingClientRect().top + window.scrollY <= y) cur = heads[i]; else break; }
      if (window.innerHeight + window.scrollY >= document.body.offsetHeight - 2) cur = heads[heads.length - 1];
      Object.keys(links).forEach(function (id) { links[id].classList.toggle("active", id === cur.id); });
    }
    var ticking = false;
    window.addEventListener("scroll", function () {
      if (ticking) return;
      ticking = true;
      requestAnimationFrame(function () { update(); ticking = false; });
    }, { passive: true });
    update();
  }

  // ---- search: ⌘K / ctrl-K / "/" opens a palette over every page ----------
  // The index is /search-index, every page rendered in one document, fetched
  // the first time the palette opens and split into one entry per heading.
  function search() {
    if (typeof HTMLDialogElement === "undefined" || !window.fetch) return;
    var masthead = document.querySelector(".masthead-right");
    var btn = document.createElement("button");
    btn.type = "button"; btn.className = "search-btn label";
    btn.innerHTML = "✿ search <kbd>/</kbd>";
    if (masthead) masthead.insertBefore(btn, masthead.firstChild);

    var dlg = document.createElement("dialog"); dlg.className = "palette";
    dlg.innerHTML = '<input type="search" placeholder="Search the docs…" aria-label="Search" autocomplete="off">' +
      '<ol role="listbox"></ol><p class="hint label"><span>↑↓ move</span><span>↵ open</span><span>esc close</span></p>';
    document.body.appendChild(dlg);
    var input = dlg.querySelector("input"), list = dlg.querySelector("ol");
    var entries = null, loading = null, results = [], active = 0;

    function load() {
      if (loading) return loading;
      loading = fetch("/search-index").then(function (r) { return r.ok ? r.text() : fetch("/search-index/").then(function (r2) { return r2.text(); }); })
        .then(function (html) {
          var d = new DOMParser().parseFromString(html, "text/html");
          entries = [];
          d.querySelectorAll("article").forEach(function (art) {
            var page = { slug: art.dataset.slug, title: art.dataset.title, section: art.dataset.section };
            var cur = { page: page, heading: "", id: "", text: art.dataset.description || "" };
            entries.push(cur);
            Array.prototype.slice.call(art.children).forEach(function (el) {
              if (/^H[23]$/.test(el.tagName)) { cur = { page: page, heading: el.textContent.trim(), id: el.id, text: "" }; entries.push(cur); }
              else cur.text += " " + el.textContent;
            });
          });
          entries.forEach(function (e) {
            e.hay = (e.page.title + " " + e.heading + " " + e.text).toLowerCase();
            e.titleLow = e.page.title.toLowerCase(); e.headLow = e.heading.toLowerCase();
          });
        });
      return loading;
    }

    function score(e, terms) {
      var s = 0;
      for (var i = 0; i < terms.length; i++) {
        var t = terms[i];
        if (e.titleLow.indexOf(t) >= 0) s += 3;
        else if (e.headLow.indexOf(t) >= 0) s += 2;
        else if (e.hay.indexOf(t) >= 0) s += 1;
        else return 0;
      }
      return s;
    }
    function snippet(e, term) {
      var i = e.text.toLowerCase().indexOf(term);
      if (i < 0) return e.text.trim().slice(0, 110);
      var start = Math.max(0, i - 40);
      return (start ? "…" : "") + e.text.slice(start, i + 80).trim();
    }
    function esc(s) { return s.replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
    function mark(s, terms) {
      var out = esc(s);
      terms.forEach(function (t) { if (t) out = out.replace(new RegExp("(" + t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")", "ig"), "<mark>$1</mark>"); });
      return out;
    }
    function render() {
      var q = input.value.trim().toLowerCase();
      var terms = q.split(/\s+/).filter(Boolean);
      list.innerHTML = "";
      results = [];
      if (!entries || !terms.length) return;
      var scored = entries.map(function (e) { return { e: e, s: score(e, terms) }; }).filter(function (x) { return x.s > 0; });
      scored.sort(function (a, b) { return b.s - a.s; });
      var groups = {}, order = [];
      scored.forEach(function (x) {
        var sec = x.e.page.section;
        if (!groups[sec]) { groups[sec] = []; order.push(sec); }
        if (groups[sec].length < 6) groups[sec].push(x.e);
      });
      if (!order.length) { list.innerHTML = '<li class="empty">Nothing for “' + esc(q) + "”.</li>"; return; }
      order.forEach(function (sec) {
        var h = document.createElement("li"); h.className = "group label"; h.textContent = sec; list.appendChild(h);
        groups[sec].forEach(function (e) {
          var li = document.createElement("li");
          var a = document.createElement("a");
          a.href = "/docs/" + e.page.slug + (e.id ? "#" + e.id : "");
          a.innerHTML = mark(e.page.title, terms) + (e.heading ? ' <span class="where">› ' + mark(e.heading, terms) + "</span>" : "") +
            '<span class="snippet">' + mark(snippet(e, terms[0]), terms) + "</span>";
          li.appendChild(a); list.appendChild(li); results.push(li);
        });
      });
      active = 0; setActive();
    }
    function setActive() {
      results.forEach(function (li, i) { li.classList.toggle("active", i === active); });
      if (results[active]) results[active].scrollIntoView({ block: "nearest" });
    }
    function open() {
      if (dlg.open) return;
      dlg.showModal();
      input.value = ""; list.innerHTML = '<li class="empty">Type to search.</li>';
      input.focus();
      load().then(function () { if (input.value) render(); }).catch(function () { list.innerHTML = '<li class="empty">Search isn’t available right now.</li>'; });
    }
    btn.addEventListener("click", open);
    input.addEventListener("input", render);
    dlg.addEventListener("keydown", function (e) {
      if (e.key === "ArrowDown") { e.preventDefault(); if (results.length) { active = (active + 1) % results.length; setActive(); } }
      else if (e.key === "ArrowUp") { e.preventDefault(); if (results.length) { active = (active - 1 + results.length) % results.length; setActive(); } }
      else if (e.key === "Enter") { e.preventDefault(); if (results[active]) location.href = results[active].querySelector("a").href; }
    });
    dlg.addEventListener("click", function (e) { if (e.target === dlg) dlg.close(); });
    document.addEventListener("keydown", function (e) {
      var typing = /^(INPUT|TEXTAREA|SELECT)$/.test((e.target.tagName || "")) || e.target.isContentEditable;
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); open(); }
      else if (e.key === "/" && !typing && !dlg.open) { e.preventDefault(); open(); }
    });
  }

  theme(); nav(); copy(); callouts(); highlight(); tabs(); rail(); toc(); search();
})();
