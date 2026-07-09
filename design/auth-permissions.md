# Auth & permissions — 0.2 design

Status: **proposal for review** (not yet implemented). Refines Friendo's
site-level identity and permission model for 0.2. It does not change the platform
(friendo.world) auth system except where the two meet.

## Why

The role model looks like a four-tier hierarchy but behaves like a two-tier one,
and it can't express the permission both real-world sites actually need.

- **`editor` is a phantom role.** Every privileged endpoint — records CRUD, media,
  users, settings, sync/deploy, comment moderation, poll creation — sits behind a
  single **admin+** gate. Members and editors are both excluded, so an editor
  account can do nothing an admin can. The docs claim editors edit content; the
  code returns 403.
- **Ownership has no integrity.** First-run creates one `superadmin`; the API can
  never *grant* superadmin, yet a superadmin can demote (then delete) another. The
  last owner can demote themselves and lock the site out. No transfer path.
- **A pure ladder can't say "edit your own, but not others'."** Two representative
  sites both hinge on this:
  - a **news blog** — editors edit any content, *contributors edit their own
    posts*, members only react/comment;
  - a **social / groups** app — everyone posts and edits their own, comments/reacts
    on any, admins moderate.
  A ranked hierarchy makes each higher role a superset of the lower, so there's no
  place for "can edit, but only what they authored." That's a second axis
  (**ownership**), orthogonal to the trust ladder.

## Shape of the model

**Capabilities under the hood, role presets on the surface, site presets for
defaults** — the WordPress/Discourse pattern. A non-developer picks a preset and
assigns friendly role names; the capability matrix stays hidden. Power-user
capability editing and custom roles are deferred (see the end).

### Capabilities (the atoms — enumerated in code, not user-editable in 0.2)

```
content.create        author a post
content.edit.own      edit/delete your own posts
content.edit.any      edit/delete anyone's posts
content.publish       set a post live (vs leaving it pending approval)
comment.moderate.own  approve/reject/delete comments on YOUR OWN posts
comment.moderate.any  approve/reject/delete any comment (staff moderation)
user.manage           create/edit users, assign roles up to your own level
site.configure        settings, sync/deploy (push/pull)
site.own              grant admin/owner, transfer ownership, destroy site
```

Commenting, reacting, and voting are the **authenticated baseline** — available to
any signed-in member, not gated by a capability. Reads of the published site are
public.

### Built-in roles = capability bundles

| Role | Capabilities (adds to the row above) |
|---|---|
| **Member** | authenticated baseline only (comment / react / vote) |
| **Contributor** | `content.create`, `content.edit.own`, `comment.moderate.own` |
| **Editor** | + `content.edit.any`, `content.publish`, `comment.moderate.any` |
| **Admin** | + `user.manage`, `site.configure` |
| **Owner** | + `site.own` |

The default bundles form a clean superset ladder — but because enforcement is
capability-based, presets and (later) custom roles can deviate without reworking
the core. `superadmin` is renamed **`owner`**.

### The ownership axis

`*.own` capabilities are satisfied only when the resource belongs to the actor;
`*.any` bypasses the check. Ownership reuses the existing accounts/profiles split
(`authors.user_id`, one account → many profiles):

- **A post is yours** when its `author_id` is one of your profiles
  (`author_id ∈ authorIDsFor(user)`).
- **A comment is yours to moderate** when its `post_id` resolves to a post that is
  yours. So a Contributor curates the threads under their own posts; an Editor
  (with `comment.moderate.any`) moderates everything.

### Capability matrix

| | Member | Contributor | Editor | Admin | Owner |
|---|:-:|:-:|:-:|:-:|:-:|
| Read site · comment · react · vote | ✓ | ✓ | ✓ | ✓ | ✓ |
| Create posts | | ✓ | ✓ | ✓ | ✓ |
| Edit/delete **own** posts | | ✓ | ✓ | ✓ | ✓ |
| Edit/delete **any** post · publish | | | ✓ | ✓ | ✓ |
| Moderate comments on **own** posts | | ✓ | ✓ | ✓ | ✓ |
| Moderate **any** comment | | | ✓ | ✓ | ✓ |
| Manage users · settings · deploy | | | | ✓ | ✓ |
| Grant admin/owner · transfer · destroy | | | | | ✓ |

### Who can assign which role

An actor can grant a role only if they hold `user.manage`, and only up to (but not
including) their own level — with `site.own` required to mint admins or owners:

| Actor \ can grant | member | contributor | editor | admin | owner |
|---|:-:|:-:|:-:|:-:|:-:|
| **Owner** | ✓ | ✓ | ✓ | ✓ | ✓ |
| **Admin** | ✓ | ✓ | ✓ | | |
| Editor / Contributor / Member | | | | | |

### Guards

- **Rank guard.** You cannot modify an account at or above your own level — except
  an owner may manage other owners (this is what makes co-owner management and
  step-down possible).
- **Last-owner guard.** The site always has ≥1 owner. Demoting or deleting the sole
  remaining owner is refused (409), self-demotion included. This *replaces* today's
  "can't delete a superadmin" SQL rule, which was circumventable by
  demote-then-delete.

### Ownership transfer

No special endpoint — with co-owners allowed, transfer is ordinary role changes
under the last-owner guard: grant `owner` to a successor, then (optionally) demote
yourself to admin (now allowed, because another owner exists).

## Per-site policy + presets

Three settings live in the existing `site_settings` table (editable in admin
Settings, seeded at setup), reusing the `GetSetting/SetSetting` helpers:

```
access.default_role        member | contributor   role for new self-serve (OTP) accounts
access.signups_enabled     true  | false          can visitors self-register at all
content.require_approval   true  | false           author posts start pending vs published
```

A **preset** is just a named bundle of those settings, chosen at `friendo init` /
first-run setup:

| Preset | default_role | signups | require_approval | Feels like |
|---|---|---|---|---|
| **Personal / solo** | — | off | — | one author; invited editors; no signups |
| **Community / social** | contributor | on | off | everyone posts own; admins moderate |
| **Blog / publication** | member | on | on | readers + comments; contributors write; editors publish |

### Publishing workflow

On `content.create`, a post's status is **`pending`** when
`content.require_approval` is on *and* the author lacks `content.publish`;
otherwise **`published`**. Editors+ publish by setting the status. This mirrors the
comment auto-approve toggle already shipped.

**Renderer change (behavior).** The public site currently renders posts of *any*
status (`QueryCollection` has no filter), so drafts/pending are visible. The public
render path must filter to `published` in both runtimes; the admin/API paths keep
seeing everything. Existing sites that (accidentally) relied on drafts showing will
change behavior — called out in the docs.

## Authentication ↔ privilege

**Email OTP is a valid login for any role.** A member promoted to a privileged role
keeps passwordless login — we don't force a password. Consequences, made explicit:

- `auth_methods` (`["password"]` / `["otp"]` / `["platform"]`) stays descriptive,
  not a gate.
- First-run creates the first **owner** *with* a password (setup already collects
  one); platform-SSO owners are passwordless by design.
- Security posture: for a passwordless privileged account, email-account security
  *is* site security — production sites should configure a real email provider (the
  OTP-echo dev fallback disables once `RESEND_API_KEY` is set).

## Platform ↔ site bridge

friendo.world collaborators (`site_members`) map to site **owners** via the SSO
handoff (`createPlatformSuperadmin` → `createPlatformOwner`, role `owner`). Scoping
platform collaborators below owner is deferred.

## Terminology

- **Owner / Admin / Editor / Contributor / Member** — the site roles above.
- **Collaborators** — platform-level co-owners (`site_members`); always say
  "collaborators" at the platform layer to avoid clashing with the member role.
- **Profiles** — display personas (`authors`), one account → many. Unchanged.
- `authors.role` is **retired** — documented as reserved/unused (dropping a column
  in SQLite/D1 is disruptive), no longer treated as meaningful.

## Deferred (post-0.2)

- User-editable capability matrix / custom roles.
- Per-collection permission scoping.
- Members deleting their own comments; inline author-side comment moderation via
  `friendo.js` (0.2 surfaces own-moderation through the scoped admin queue).
- Session management UI; multiple auth methods per account; platform collaborators
  scoped below owner.
