// Thin REST client for the Friendo site API. Every Friendo runtime (Go, edge)
// exposes the same endpoints under /_/api, so this client is runtime-agnostic.

export type Role = "owner" | "admin" | "editor" | "contributor" | "member";

// Capability model mirrored from the runtime (runtime/go/data).
// Used to gate the admin UI; the server enforces the real checks.
export type Capability =
  | "content.create"
  | "content.edit.any"
  | "comment.moderate.own"
  | "user.manage"
  | "site.configure"
  | "site.own";

const ROLE_CAPS: Record<Role, Capability[]> = {
  member: [],
  contributor: ["content.create", "comment.moderate.own"],
  editor: ["content.create", "content.edit.any", "comment.moderate.own"],
  admin: ["content.create", "content.edit.any", "comment.moderate.own", "user.manage", "site.configure"],
  owner: ["content.create", "content.edit.any", "comment.moderate.own", "user.manage", "site.configure", "site.own"],
};

export function can(role: Role, cap: Capability): boolean {
  return (ROLE_CAPS[role] || []).includes(cap);
}

export type User = {
  id: string;
  email: string;
  name: string;
  role: Role;
  created?: string;
};

export type UserInput = {
  email?: string;
  name: string;
  role: string;
  password?: string;
};

// What a cold client needs to know: does the site still need its first owner, is
// an old single-admin password waiting to be upgraded, may people sign in with a
// password here, and will a sign-in code actually be emailed (else it's shown /
// printed for local dev).
export type SetupStatus = {
  needsSetup: boolean;
  hasLegacyAdmin: boolean;
  passwordLogin?: boolean;
  emailConfigured?: boolean;
};

export type AccessSettings = {
  default_role: "member" | "contributor";
  signups_enabled: boolean;
  require_approval: boolean;
  password_login: boolean;
};

// A request-code response: the code is only present in local dev (echo mode).
export type CodeSent = { sent: boolean; emailed?: boolean; code?: string };

export type Settings = {
  site: { name: string };
  collections: number;
  users: number;
  moderation: { auto_approve: boolean };
  access: AccessSettings;
  content: { accept_submissions: boolean };
  // DB keys frozen by friendo.toml's [settings] block — rendered read-only.
  managed: string[];
};

export type SettingsPatch = {
  moderation?: { auto_approve: boolean };
  access?: Partial<AccessSettings>;
  content?: { accept_submissions?: boolean };
};

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch("/_/api" + path, {
    credentials: "same-origin",
    // The admin identifies itself: on localhost with no email provider, the
    // runtime opens the admin without a sign-in for these calls only — a site's
    // own <friendo-*> tags never send this, so visitors stay visitors.
    headers: { "X-Friendo-Admin": "1", ...(opts.body ? { "Content-Type": "application/json" } : {}) },
    ...opts,
  });
  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export type Collection = { name: string; count: number };

// A post's calendar time, as the API returns it (null for a post with no time).
export type When = {
  starts: string; // RFC 3339, in the event's zone
  ends: string; // "" when open-ended
  all_day: boolean;
  timezone: string;
  repeats: string; // human text: "weekly", "monthly on the first Tuesday", ""
  rule: string; // the RRULE body, "" for a one-off
  except: string[];
  next: { starts: string; ends: string; all_day: boolean } | null;
};

export type Record = {
  id: string;
  collection?: string;
  slug: string;
  title: string;
  body: string;
  status: string;
  author_id?: string;
  published_at?: string;
  created?: string;
  updated?: string;
  data?: { [key: string]: unknown };
  when?: When | null;
};

export type RecordInput = {
  slug: string;
  title: string;
  body: string;
  status: string;
  // Front-matter style fields. The reserved calendar keys (when, ends, all_day,
  // timezone, repeats, except, rrule) are lifted into the post's event server-side.
  data?: { [key: string]: unknown };
};

export type PendingRecord = {
  id: string;
  collection: string;
  slug: string;
  title: string;
  author_id: string;
  author_name: string;
  status: string;
  created: string;
  when?: When | null;
};

export type Attendee = {
  id: string;
  occurrence: string;
  occurrence_text: string;
  scheduled: boolean;
  author_id: string;
  author_name: string;
  author_email: string;
  answer: "going" | "not_going" | "maybe";
  created: string;
  updated: string;
};

export type Location = {
  id: string;
  target_type: string;
  target_id: string;
  lat: number;
  lng: number;
  label: string;
};

export type CommentStatus = "pending" | "approved" | "rejected";

export type Comment = {
  id: string;
  post_id: string;
  parent_id: string;
  author_id: string;
  author_name: string;
  author_avatar: string;
  body: string;
  status: CommentStatus;
  created: string;
};

export const api = {
  me: () => req<{ user: User }>("/me"),
  login: (email: string, password: string) =>
    req<{ user: User }>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  // Passwordless sign-in: ask for a code, then verify it.
  requestCode: (email: string) =>
    req<CodeSent>("/auth/request-code", { method: "POST", body: JSON.stringify({ email }) }),
  verifyCode: (email: string, code: string) =>
    req<{ user: User }>("/auth/verify-code", {
      method: "POST",
      body: JSON.stringify({ email, code }),
    }),
  logout: () => req<void>("/auth/logout", { method: "POST" }),

  collections: () => req<{ collections: Collection[] }>("/collections"),
  records: (collection: string) =>
    req<{ records: Record[] }>(`/collections/${encodeURIComponent(collection)}/records`),
  record: (id: string) => req<{ record: Record }>(`/records/${encodeURIComponent(id)}`),
  createRecord: (collection: string, input: RecordInput) =>
    req<{ record: Record }>(`/collections/${encodeURIComponent(collection)}/records`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateRecord: (id: string, input: RecordInput) =>
    req<{ record: Record }>(`/records/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteRecord: (id: string) =>
    req<void>(`/records/${encodeURIComponent(id)}`, { method: "DELETE" }),

  // Who answered an event's RSVP (the post's author or a moderator).
  attendees: (recordId: string) =>
    req<{ attendees: Attendee[] }>(`/posts/${encodeURIComponent(recordId)}/attendees`),

  // A post's map pin (the Where section of the record form).
  locations: (recordId: string) =>
    req<{ locations: Location[] }>(`/locations?target_type=post&target_id=${encodeURIComponent(recordId)}`),
  addLocation: (recordId: string, lat: number, lng: number, label: string) =>
    req<{ location: Location }>("/locations", {
      method: "POST",
      body: JSON.stringify({ target_type: "post", target_id: recordId, lat, lng, label }),
    }),
  removeLocation: (id: string) =>
    req<void>(`/locations/${encodeURIComponent(id)}`, { method: "DELETE" }),

  // Post-review queue (editor+)
  recordsByStatus: (status: string) =>
    req<{ records: PendingRecord[] }>(`/records?status=${encodeURIComponent(status)}`),
  setRecordStatus: (id: string, status: string) =>
    req<{ record: Record }>(`/records/${encodeURIComponent(id)}/status`, {
      method: "PUT",
      body: JSON.stringify({ status }),
    }),

  setupStatus: () => req<SetupStatus>("/setup"),
  // First-run setup: a code proves the owner's email; no account exists until
  // it verifies. Setting a password instead also turns password sign-in on.
  setupRequestCode: (email: string) =>
    req<CodeSent>("/setup/request-code", { method: "POST", body: JSON.stringify({ email }) }),
  setupWithCode: (email: string, name: string, code: string) =>
    req<{ user: User }>("/setup", {
      method: "POST",
      body: JSON.stringify({ email, name, code }),
    }),
  setup: (email: string, name: string, password: string) =>
    req<{ user: User }>("/setup", {
      method: "POST",
      body: JSON.stringify({ email, name, password }),
    }),
  migrate: (email: string, password: string) =>
    req<{ ok: boolean }>("/migrate", { method: "POST", body: JSON.stringify({ email, password }) }),

  users: () => req<{ users: User[] }>("/users"),
  createUser: (input: UserInput) =>
    req<{ user: User }>("/users", { method: "POST", body: JSON.stringify(input) }),
  updateUser: (id: string, input: UserInput) =>
    req<{ user: User }>(`/users/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteUser: (id: string) =>
    req<void>(`/users/${encodeURIComponent(id)}`, { method: "DELETE" }),

  settings: () => req<Settings>("/settings"),
  updateSettings: (patch: SettingsPatch) =>
    req<Settings>("/settings", {
      method: "PUT",
      body: JSON.stringify(patch),
    }),

  // Comment moderation
  comments: (status: CommentStatus = "pending") =>
    req<{ comments: Comment[] }>(`/comments?status=${encodeURIComponent(status)}`),
  setCommentStatus: (id: string, status: CommentStatus) =>
    req<{ comment: Comment }>(`/comments/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify({ status }),
    }),
  deleteComment: (id: string) =>
    req<void>(`/comments/${encodeURIComponent(id)}`, { method: "DELETE" }),
};

// availableRoles returns the roles an actor may assign. Granting admin/owner
// needs site.own (owners only); lower roles need user.manage. Mirrors the runtimes.
export function availableRoles(actorRole: Role): Role[] {
  if (!can(actorRole, "user.manage")) return [];
  const roles: Role[] = ["member", "contributor", "editor"];
  if (can(actorRole, "site.own")) roles.push("admin", "owner");
  return roles;
}
