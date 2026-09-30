// Thin REST client for the Friendo site API. Every Friendo runtime (Go, edge)
// exposes the same endpoints under /_/api, so this client is runtime-agnostic.

export type Role = "owner" | "admin" | "editor" | "moderator" | "contributor" | "member";

// Capability model mirrored from the runtime (runtime/go/data).
// Used to gate the admin UI; the server enforces the real checks.
export type Capability =
  | "content.create"
  | "content.edit.own"
  | "content.edit.any"
  | "content.publish"
  | "review.own"
  | "review.any"
  | "review.posts"
  | "user.manage"
  | "site.configure"
  | "site.own";

// Each role holds everything the one below it does, exactly as the server grants
// them (runtime/go/data/data.go, roleCapabilities).
const CONTRIBUTOR: Capability[] = ["content.create", "content.edit.own", "review.own"];
const MODERATOR: Capability[] = [...CONTRIBUTOR, "review.any", "review.posts"];
const EDITOR: Capability[] = [...MODERATOR, "content.edit.any", "content.publish"];
const ADMIN: Capability[] = [...EDITOR, "user.manage", "site.configure"];
const ROLE_CAPS: { [role in Role]: Capability[] } = {
  member: [],
  contributor: CONTRIBUTOR,
  moderator: MODERATOR,
  editor: EDITOR,
  admin: ADMIN,
  owner: [...ADMIN, "site.own"],
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
  // The persona attribution flows to (GET /me), and its address (GET /users).
  author_id?: string;
  profile_slug?: string;
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


// A request-code response: the code is only present in local dev (echo mode).
export type CodeSent = { sent: boolean; emailed?: boolean; code?: string };

// The community features a site can switch off (Settings → Features). Off means
// the API refuses it, its <friendo-*> tag renders nothing, and the admin hides
// its section; existing data is kept.
export type Features = {
  comments: boolean;
  reactions: boolean;
  polls: boolean;
  rsvp: boolean;
  locations: boolean;
  chats: boolean;
  follows: boolean;
  groups: boolean;
};

export const ALL_FEATURES_ON: Features = { comments: true, reactions: true, polls: true, rsvp: true, locations: true, chats: true, follows: true, groups: true };

// Who may see member profiles (/profiles/<slug> and the profiles API).
export type ProfileVisibility = "members" | "public";

// Every setting by its one name — the same word in friendo.toml's [settings]
// block, in the database and in the API.
export type SettingValues = {
  open_signups: boolean;
  signups_are_contributors: boolean;
  members_can_post: boolean;
  posts_need_review: boolean;
  comments_need_review: boolean;
  password_login: boolean;
  members_can_start_groups: boolean;
  // What a visitor (not signed in) may do; each is off by default.
  visitors_can_react: boolean;
  visitors_can_vote: boolean;
  visitors_can_rsvp: boolean;
  profile_visibility: ProfileVisibility;
  // Which built-in collections show when friendo.toml lists no [content] collections.
  default_collections: string[];
};

export type Settings = SettingValues & {
  site: { name: string };
  collections: number;
  users: number;
  collections_declared: boolean;
  features: Features;
  // Keys frozen by friendo.toml's [settings] block — rendered read-only.
  managed: string[];
};

export type SettingsPatch = Partial<SettingValues> & {
  features?: Partial<Features>;
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
    // A FormData body (file upload) sets its own multipart content type.
    headers: {
      "X-Friendo-Admin": "1",
      ...(opts.body && !(opts.body instanceof FormData) ? { "Content-Type": "application/json" } : {}),
    },
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

// A field friendo.toml declares for a content type ([content.<type>.fields]).
export type DeclaredField = {
  name: string;
  kind: string; // text | paragraph | number | checkbox | tags | image | json
  choices?: string[];
  required?: boolean;
  hint?: string;
};

// `declared` marks a collection named in friendo.toml's [content] types; the rest
// exist because they have records (or are the built-in defaults).
export type Collection = { name: string; count: number; declared: boolean; fields: DeclaredField[] };

// A post's time, as the API returns it (null for a post with no time).
export type When = {
  start: string; // RFC 3339, in the event's zone
  end: string; // "" when open-ended
  all_day: boolean;
  timezone: string;
  repeats: string; // human text: "weekly", "monthly on the first Tuesday", ""
  rule: string; // the RRULE body, "" for a one-off
  except: string[];
  next: { start: string; end: string; all_day: boolean } | null;
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
  fields?: { [key: string]: unknown };
  when?: When | null;
};

export type RecordInput = {
  slug: string;
  title: string;
  body: string;
  status: string;
  // Front-matter style fields. `when` is lifted into the post's event server-side.
  fields?: { [key: string]: unknown };
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

export type RSVPName = {
  id: string;
  date: string;
  date_text: string;
  scheduled: boolean;
  author_id: string;
  author_name: string;
  author_email: string;
  answer: "going" | "not_going" | "maybe" | "invited";
  created: string;
  updated: string;
};

// The result of inviting people to an event: how many were asked, how many had
// already answered, and any addresses that matched nobody.
export type InviteResult = { invited: number; skipped: number; unknown: string[]; counts: { going: number; maybe: number; not_going: number; invited: number } };

export type Location = {
  id: string;
  target_type: string;
  target_id: string; // the post
  lat: number;
  lng: number;
  label: string;
};

// An uploaded image attached to a record. `field` names the record field it
// belongs to ("photo", "gallery"); `url` is where it's served from.
export type FileRow = {
  id: string;
  record_type: string;
  record_id: string;
  field: string;
  url: string;
  mime: string;
  size: number;
  created: string;
};

// A member of a group: their profile plus their standing in it.
export type GroupMember = {
  id: string;
  slug: string;
  name: string;
  avatar: string;
  role: "admin" | "moderator" | "member";
  status: "member" | "requested" | "invited";
  since: string;
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
  // Public: which community features the site has on.
  features: () => req<{ features: Features }>("/features"),

  // `declared` says whether friendo.toml has a [content] collections list at all;
  // `profile_fields` are the [profiles] fields a member's profile carries.
  collections: () =>
    req<{ collections: Collection[]; declared: boolean; profile_fields: DeclaredField[] }>("/collections"),
  // The [content] block that matches the site as it is, to paste into friendo.toml.
  contentToml: () => req<{ toml: string }>("/content/toml"),
  records: (collection: string) =>
    req<{ posts: Record[] }>(`/collections/${encodeURIComponent(collection)}/posts`),
  record: (id: string) => req<{ post: Record }>(`/posts/${encodeURIComponent(id)}`),
  createRecord: (collection: string, input: RecordInput) =>
    req<{ post: Record }>(`/collections/${encodeURIComponent(collection)}/posts`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateRecord: (id: string, input: RecordInput) =>
    req<{ post: Record }>(`/posts/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteRecord: (id: string) =>
    req<void>(`/posts/${encodeURIComponent(id)}`, { method: "DELETE" }),

  // Who answered an event's RSVP (the post's author or a moderator).
  rsvpNames: (recordId: string) =>
    req<{ names: RSVPName[] }>(`/posts/${encodeURIComponent(recordId)}/rsvps/names`),
  // Ask people to an event: profile names, a group's members, or your followers.
  invite: (recordId: string, body: { slugs?: string[]; group?: string; followers?: boolean; date?: string }) =>
    req<InviteResult>(`/posts/${encodeURIComponent(recordId)}/invites`, { method: "POST", body: JSON.stringify(body) }),

  // A post's location (the Location section of the post form).
  locations: (recordId: string) =>
    req<{ locations: Location[] }>(`/locations?post_id=${encodeURIComponent(recordId)}`),
  addLocation: (recordId: string, lat: number, lng: number, label: string) =>
    req<{ location: Location }>("/locations", {
      method: "POST",
      body: JSON.stringify({ post_id: recordId, lat, lng, label }),
    }),
  removeLocation: (id: string) =>
    req<void>(`/locations/${encodeURIComponent(id)}`, { method: "DELETE" }),

  // Images attached to a post. Uploading needs the post to exist first, so a
  // new post is saved, then its images go up, then the URLs are written back.
  files: (recordId: string) =>
    req<{ files: FileRow[] }>(`/files?post_id=${encodeURIComponent(recordId)}`),
  uploadFile: (recordId: string, field: string, file: File) => {
    const fd = new FormData();
    fd.append("post_id", recordId);
    fd.append("field", field);
    fd.append("file", file);
    return req<{ file: FileRow }>("/files", { method: "POST", body: fd });
  },
  deleteFile: (id: string) => req<void>(`/files/${encodeURIComponent(id)}`, { method: "DELETE" }),

  // The review queue (moderator+)
  recordsByStatus: (status: string) =>
    req<{ posts: PendingRecord[] }>(`/posts?status=${encodeURIComponent(status)}`),
  setRecordStatus: (id: string, status: string) =>
    req<{ post: Record }>(`/posts/${encodeURIComponent(id)}/status`, {
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

  // A group's members (the Members section of a group record).
  groupMembers: (groupId: string, status: "member" | "requested" = "member") =>
    req<{ members: GroupMember[] }>(`/groups/${encodeURIComponent(groupId)}/members?status=${status}`),
  addGroupMember: (groupId: string, slug: string, role: "admin" | "moderator" | "member" = "member") =>
    req<{ members: GroupMember[] }>(`/groups/${encodeURIComponent(groupId)}/members`, {
      method: "POST",
      body: JSON.stringify({ slug, role }),
    }),
  setGroupMember: (groupId: string, authorId: string, patch: { status?: string; role?: string }) =>
    req<{ members: GroupMember[]; requested: GroupMember[] }>(
      `/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(authorId)}`,
      { method: "PUT", body: JSON.stringify(patch) }
    ),
  removeGroupMember: (groupId: string, authorId: string) =>
    req<void>(`/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(authorId)}`, { method: "DELETE" }),

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
  const roles: Role[] = ["member", "contributor", "moderator", "editor"];
  if (can(actorRole, "site.own")) roles.push("admin", "owner");
  return roles;
}
