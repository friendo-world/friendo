# Parity tests

Friendo's core bet is *one admin UI, two runtimes, identical behavior*. These
tests encode that guarantee across three layers, each driven by a shared fixture
file run against **both** runtimes:

| Layer | Fixtures | Go runner | Edge runner |
|---|---|---|---|
| **REST API** | [`scenarios.json`](scenarios.json) | [`parity_test.go`](parity_test.go) | [`edge-parity.mjs`](edge-parity.mjs) |
| **Template render** | [`render-scenarios.json`](render-scenarios.json) | `render_test.go` | `edge-parity.mjs` |
| **SQL splitter** | [`splitter-cases.json`](splitter-cases.json) | `splitter_test.go` | `edge-parity.mjs` |

- The Go runner mounts the real router in-process (`httptest`) over a fresh temp
  SQLite DB; render_test.go mounts the real site handler (`server.BuildSiteHandler`).
- The edge runner boots `wrangler dev` in an isolated `--persist-to` dir (fresh D1,
  schema auto-created on first request) and drives it over HTTP.

Each REST run is self-contained: it starts from an **empty database**, calls
`/setup` to create the admin, then exercises records CRUD, users + role
enforcement, settings, and the session lifecycle — so the same scenarios are valid
on both.

**Render fixtures** push a template + records and assert the rendered output equals
a golden `expect` (so matching the golden on both sides proves the two template
engines render identically — nesting, `forloop.*`, autoescape, filters,
conditionals, published-only filtering). Fixtures are fragments (no `</body>`) so
the Go serve path's dev-only live-reload injection stays a no-op.

**Splitter cases** feed a multi-statement SQL string through each runtime's schema
statement splitter and assert identical output (guards the historical
comment-led-file bug). The **CLI sync round-trip** (`init → push → pull`) is covered
separately in [`cli/internal/deploy/roundtrip_test.go`](../cli/internal/deploy/roundtrip_test.go),
driving the real deploy client against an in-process runtime.

## Running

```bash
npm test            # both runtimes
npm run test:go     # Go only          (fast; no Cloudflare tooling needed)
npm run test:edge   # edge only        (boots wrangler; ~20–40s)
```

`test:go` is also a plain `go test ./tests/`.

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

- **`path`** is appended to each runtime's `/_/api` base.
- **`expect.json`** maps a dotted path (with numeric array indices, e.g.
  `collections.0.name`) to an exact expected value.
- **`expect.length`** asserts array lengths.
- **`capture`** saves a response field into a variable; reference it later as
  `${name}` in any `path`, `body`, or expected value.
- A session cookie is tracked automatically across steps (set by `/setup` and
  `/auth/login`, cleared by `/auth/logout`).

Both runners are thin interpreters of this format, kept deliberately simple so
they don't drift. To extend coverage, add a step to `scenarios.json` — it runs
against both runtimes automatically. Keep assertions deterministic (don't assert
generated ids or timestamps directly; capture ids instead).
