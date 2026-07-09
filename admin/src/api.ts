// Thin REST client for the Friendo site API. Every Friendo runtime (Go, edge)
// exposes the same endpoints under /_/api, so this client is runtime-agnostic.

export type Role = "superadmin" | "admin" | "editor" | "member";

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

export type SetupStatus = { needsSetup: boolean; hasLegacyAdmin: boolean };

export type Settings = {
  site: { name: string };
  collections: number;
  users: number;
  moderation: { auto_approve: boolean };
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
    headers: opts.body ? { "Content-Type": "application/json" } : {},
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
};

export type RecordInput = {
  slug: string;
  title: string;
  body: string;
  status: string;
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

  setupStatus: () => req<SetupStatus>("/setup"),
  setup: (email: string, name: string, password: string) =>
    req<{ user: User }>("/setup", {
      method: "POST",
      body: JSON.stringify({ email, name, password }),
    }),
  migrate: (email: string) =>
    req<{ ok: boolean }>("/migrate", { method: "POST", body: JSON.stringify({ email }) }),

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
  updateSettings: (patch: { moderation?: { auto_approve: boolean } }) =>
    req<{ moderation: { auto_approve: boolean } }>("/settings", {
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

// availableRoles returns the roles an actor with the given role may assign.
export function availableRoles(actorRole: Role): Role[] {
  switch (actorRole) {
    case "superadmin":
      return ["admin", "editor", "member"];
    case "admin":
      return ["editor", "member"];
    default:
      return ["member"];
  }
}
