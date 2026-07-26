# Regression tests

These fixtures pin the runtime's behavior across three layers. Each is a shared
JSON fixture run against the Go runtime in-process. (They began as a Go/edge parity
harness; the edge runtime has been retired, so they now guard the one runtime.)

| Layer | Fixtures | Runner |
|---|---|---|
| **REST API** | [`scenarios.json`](scenarios.json) | [`parity_test.go`](parity_test.go) |
| **Template render** | [`render-scenarios.json`](render-scenarios.json) | [`render_test.go`](render_test.go) |
| **SQL splitter** | [`splitter-cases.json`](splitter-cases.json) | [`splitter_test.go`](splitter_test.go) |

- The REST runner mounts the real router in-process (`httptest`) over a fresh temp
  SQLite DB; `render_test.go` mounts the real site handler (`server.BuildSiteHandler`).

Each REST run is self-contained: it starts from an **empty database**, calls
`/setup` to create the admin, then exercises records CRUD, users + role enforcement,
settings, and the session lifecycle.

**Render fixtures** push a template + records and assert the rendered output equals a
golden `expect` (nesting, `forloop.*`, autoescape, filters, conditionals,
published-only filtering). Fixtures are fragments (no `</body>`) so the serve path's
dev-only live-reload injection stays a no-op.

**Splitter cases** feed a multi-statement SQL string through the schema statement
splitter and assert its output (guards the historical comment-led-file bug). The
**CLI sync round-trip** (`init → push → pull`) is covered separately in
[`cli/internal/deploy/roundtrip_test.go`](../cli/internal/deploy/roundtrip_test.go),
driving the real deploy client against an in-process runtime.

**SDK browser check** ([`sdk-map.mjs`](sdk-map.mjs), `npm run test:sdk`) covers what
a REST/render harness can't: the `friendo.js` Web Components only render in a browser.
It boots the runtime over a throwaway site, seeds locations, and drives Chromium
(Playwright) to assert `<friendo-map>` paints its markers. It needs network (Leaflet +
OSM tiles load from a CDN), so it is **not** part of `npm test` — run it on demand.

## Running

```bash
npm test            # the Go test suite
npm run test:go     # same — go test ./...
```

## The scenario format

Each step is one request and its expectations:

```json
{
  "name": "create a record",
  "method": "POST",
  "path": "/collections/blog/records",
  "body": { "title": "Hello", "slug": "hello", "body": "...", "status": "published" },
  "expect": {
    "status": 201,
    "json":   { "record.title": "Hello", "record.collection": "blog" },
    "length": { "records": 1 }
  },
  "capture": { "recordId": "record.id" }
}
```

- **`path`** is appended to the `/_/api` base.
- **`expect.json`** maps a dotted path (with numeric array indices, e.g.
  `collections.0.name`) to an exact expected value.
- **`expect.length`** asserts array lengths.
- **`capture`** saves a response field into a variable; reference it later as
  `${name}` in any `path`, `body`, or expected value.
- A session cookie is tracked automatically across steps (set by `/setup` and
  `/auth/login`, cleared by `/auth/logout`).

The runner is a thin interpreter of this format. To extend coverage, add a step to
`scenarios.json`. Keep assertions deterministic (don't assert generated ids or
timestamps directly; capture ids instead).
