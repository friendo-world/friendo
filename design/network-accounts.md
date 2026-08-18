# Network accounts, device-auth, SSO, and self-service deploy

## Context

The single-runtime pivot deleted the Cloudflare Better-Auth "platform" that used to hold user
accounts, run **device-auth** for the CLI, and **SSO** the deploying user into their sites.
Network mode today has only **operators** (a password login + `friendo network login`) who
provision sites. To make `friendo deploy` work for **everyday users** — "publish my folder to
friendo.world" — the network must reinstate that account / device-auth / SSO layer, now in Go,
and the CLI should split cleanly into two tiers.

## Decisions (locked with the user)

- **One account system, capabilities on top.** A single network account; `operator` is a
  capability. An operator is just an account that can *also* manage the network (and can own
  sites like anyone else).
- **Device-auth is the only login path.** **Retire the operator password path**
  (`FRIENDO_OPERATOR_PASSWORD`, `friendo network login` w/ password). In-browser account auth is
  **passwordless OTP**, reusing friendo's existing member OTP → requires email delivery (Resend;
  dev falls back to the OTP echo).
- **Two-tier CLI:** `friendo deploy` = everyday users (friendo.world default, `--network`
  override); `friendo network *` = operators (require the `operator` capability).
- **Signups: operator-configurable, default invite-only.**

## Identity model

- **Account** (network-level): `id, email, capabilities, created`. Distinct from a *site's* own
  users. One account owns many sites.
- **Capabilities:** `operator` (manage the network). Regular accounts can create/own sites (per
  signup policy) but can't operate the network.
- **Site ownership:** the sites registry gains `owner_account_id`. Provisioning records the
  creating account as owner; operators see/manage all sites.
- **Account ↔ site-user bridge:** a site (tenant) has its own `users` (owner/admin/…). On
  provision, the network creates the site's first **owner user linked to the account**, so a
  site session can be issued for that owner on deploy (the "SSO").

## Device-auth flow (OAuth-device-style; port the old platform's flow into Go)

Served by the network on the apex:
1. `POST /api/auth/device/start` → `{ device_code, user_code, verification_uri, interval, expires_in }`.
2. CLI opens the browser to `verification_uri` (e.g. `https://friendo.world/activate?code=USER_CODE`)
   and polls `POST /api/auth/device/poll { device_code }` (→ `authorization_pending` until
   approved, then `{ token }`).
3. **Browser approval page** (`GET /activate`): user loads/enters the `user_code`, authenticates
   the account (email → **OTP code** → verify; creates the account if signups allow), and approves.
4. The poll returns an **account session token**; the CLI caches it in `~/.friendo/config` (reuse
   the `Networks` map, keyed by network URL).

## Site session for deploy ("SSO", simplified in-process)

The old model needed cross-Worker SSO codes (platform Worker → site Worker). In network mode it's
one process, so it collapses:
- `POST /api/sso/exchange { subdomain }` (account-auth): the network verifies the account **owns**
  `subdomain`, ensures the site's owner user, issues a **site admin session** (`friendo_session`),
  and returns it. No shared-secret round-trip needed.
- `friendo deploy` then pushes with that session (the existing `/push/*` endpoints).
- Keep the `/api/sso/exchange` *shape* for a clean boundary + the per-site isolation escape hatch,
  even though it resolves in-process today.

## Self-service site creation + signup policy

- `POST /api/sites { subdomain, name }` (account-auth): create a site **owned by the account** if
  the subdomain is free and policy allows; idempotent for a site you already own. Enforces
  per-account **quotas** (operator-set).
- Network setting `signups = invite | open` (operator-configurable; default **invite**). Invite
  mode: signup during device approval needs a valid invite; `POST /api/invites` (operator) mints them.

## First-operator bootstrap (password retired)

- `FRIENDO_OPERATOR_EMAIL` (kept) designates the bootstrap operator: on first boot that email is
  pre-granted the `operator` capability. They sign in via device-auth/OTP — **no password**. Drop
  `FRIENDO_OPERATOR_PASSWORD`.
- **Safety net (shipped):** if *no* account holds the operator capability yet, the first person to
  complete the console OTP sign-in claims it (mirrors first-run setup). So a network is reachable
  even without `FRIENDO_OPERATOR_EMAIL`, and a misconfigured redeploy can't lock the console out.
- **friendo.world migration:** keep the existing operator's email, grant it `operator` (env), drop its
  password hash; the first OTP login re-establishes it. (Requires Resend configured on friendo.world.)

## CLI reshape

| Command | Who | Does |
|---|---|---|
| `friendo deploy [site] [--network URL]` | everyday user | device-auth (if needed) → create/claim `site` (owned by you) → SSO session → push. Default network **friendo.world**. Replaces the old platform deploy. |
| `friendo whoami [--network URL]` | anyone | `GET /api/account` — your account + owned sites |
| `friendo logout` | anyone | clears cached account tokens (already clears `Networks`) |
| `friendo network serve` | operator (on the box) | run the network |
| `friendo network sites / suspend / destroy / rollout` | operator cap | fleet management (all sites) |
| `friendo network accounts / invites / signups / grant` | operator cap | manage accounts, invites, signup policy, grant capabilities |

- The operator token is just an account token **with the `operator` capability** — `friendo
  network *` returns 403 without it. **Retire** `friendo network login` (password) and
  `friendo network operator add`; operators are designated by capability (env bootstrap, or
  `friendo network grant <email> operator` by an existing operator).
- Reconcile the legacy `redeploy`/`destroy` (old WfP platform) — fold into `deploy`/`network`.

## Backend changes

- Generalize `network.Operators` → an **accounts store** (`accounts`, `account_sessions`,
  `device_codes`, `invites`; `accounts.capabilities`). Reuse the runtime's OTP (`otp_codes`,
  request/verify) for in-browser account auth.
- Sites registry: add `owner_account_id`; record owner on provision; ownership checks on
  create/destroy/sso.
- New apex endpoints: `/api/auth/device/*`, `GET /activate` (HTML), `/api/account`,
  `/api/sso/exchange`, `/api/invites`, the signup-policy setting. Gate `/api/sites` create by
  account + policy; operator endpoints by capability.
- On provision, create the site's owner user linked to the account (so SSO can issue sessions).

## Reuse / head-start (as-built)

- **CLI client:** the device-auth + SSO client now lives in `cli/internal/deploy/account.go`
  (device flow + `/api/sso/exchange`); the old Cloudflare `platformClient`/`SSOCode` path has been removed.
- **Server logic:** the device-auth + SSO + account shapes that `platform/worker.js` prototyped in
  JS were ported into Go (`runtime/go/network`); `platform/` has since been deleted.
- **OTP:** reuses the runtime's passwordless OTP (`/_/api/auth/request-code|verify-code`).
- **Operator half:** the current `network.Operators` + console + operator `/api/*` are the seed to
  generalize into accounts.

## Phasing (each independently shippable)

1. ✅ **Accounts + device-auth + `friendo whoami`.** (shipped) Generalized store; device-flow endpoints +
   `/activate` OTP page; CLI device-auth. Operator = account with the capability; bootstrap via
   `FRIENDO_OPERATOR_EMAIL`.
2. ✅ **Ownership + self-service `friendo deploy`.** (shipped) Registry `owner_account_id`; account-gated
   `POST /api/account/sites` + `/api/sso/exchange` (in-process site session); `friendo deploy`
   (friendo.world default, `--network`). Signup default invite-only + `network invite` + a console UI
   (signups toggle, invite form, owner column). R2 media covers uploads **and** content galleries.
3. ✅ **Reconcile + retire.** (shipped) Removed the operator password store (`network.Operators`) and the
   whole WfP platform-client subtree (`PlatformClient`, `platformClient`, SSO-code, device-auth, the
   legacy `redeploy`/`destroy` commands + `RunDeploy`/`RunWhoami`). Console sign-in is OTP; the operator
   API is gated on the `operator` capability (401 unauth / 403 non-operator). `friendo network login`
   (password) and `operator add` are gone → `friendo login` + `friendo network operator grant`. First
   sign-in claims operator when none exists (see bootstrap net above).
4. **Polish (next).** Quotas, `network accounts`/`signups` management beyond the current console UI,
   and custom domains — scoped as **v0.4**: [v0.4-roadmap.md](v0.4-roadmap.md).

## Open items

- **Email delivery is now required for login** (OTP) — confirm Resend on friendo.world; dev uses
  the OTP echo.
- Quotas model (per-account site count / storage) — first cut can be a simple count cap.
  Scoped in [v0.4-roadmap.md](v0.4-roadmap.md) (Tier A); storage accounting is still undecided.
- Operator **support-access** into sites owned by others — ties to the earlier "power boundary"
  decision; keep it explicit + audit-logged, not implicit.
