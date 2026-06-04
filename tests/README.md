# Parity tests

Friendo's core bet is *one admin UI, two runtimes, identical behavior*. These
tests encode that guarantee: a single [`scenarios.json`](scenarios.json) of
request→assert steps is run against **both** runtimes, and both must agree.

| | Runner | What it does |
|---|---|---|
| Go runtime | [`parity_test.go`](parity_test.go) | Mounts the real router in-process (`httptest`) over a fresh temp SQLite DB |
| Edge runtime | [`edge-parity.mjs`](edge-parity.mjs) | Boots `wrangler dev` in an isolated `--persist-to` dir (fresh D1, schema auto-created on first request), drives it over HTTP |

Each run is self-contained: it starts from an **empty database**, calls `/setup`
to create the admin, then exercises records CRUD, users + role enforcement,
settings, and the session lifecycle — so the same scenarios are valid on both.

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
