// Thin REST client for the Friendo site API. Every Friendo runtime (Go, edge)
// exposes the same endpoints under /_/api, so this client is runtime-agnostic.

export type User = {
  id: string;
  email: string;
  name: string;
  role: "superadmin" | "admin" | "editor" | "member";
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
};
