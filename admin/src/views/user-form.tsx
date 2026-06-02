import { useEffect, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { api, availableRoles, type UserInput } from "../api";
import { useAuth } from "../auth";

const field =
  "mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none";

export function UserForm({ id }: { id?: string }) {
  const { user: me } = useAuth();
  const { route } = useLocation();
  const editing = !!id;
  const roles = availableRoles(me.role);

  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState<string>(roles[roles.length - 1]);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(editing);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!editing) return;
    api
      .users()
      .then((r) => {
        const u = r.users.find((x) => x.id === id);
        if (!u) {
          setError("User not found.");
          return;
        }
        setEmail(u.email);
        setName(u.name);
        setRole(u.role);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load user."))
      .finally(() => setLoading(false));
  }, [id]);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const input: UserInput = editing
        ? { name, role, ...(password ? { password } : {}) }
        : { email, name, role, password };
      if (editing) await api.updateUser(id!, input);
      else await api.createUser(input);
      route("/_/users");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save failed.");
      setBusy(false);
    }
  }

  if (loading) {
    return <div class="mx-auto max-w-3xl px-4 py-8 text-sm text-gray-400">Loading…</div>;
  }

  return (
    <div class="mx-auto max-w-3xl px-4 py-8">
      <h1 class="mb-6 text-xl font-bold">{editing ? "Edit user" : "Add user"}</h1>
      {error && <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>}
      <div class="rounded-lg bg-white p-6 shadow-sm">
        <form onSubmit={submit}>
          {editing ? (
            <div class="mb-4 text-sm">
              <span class="font-medium text-gray-500">Email:</span> <span>{email}</span>
            </div>
          ) : (
            <label class="mb-4 block text-sm font-medium">
              Email
              <input type="email" required value={email}
                onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
            </label>
          )}
          <label class="mb-4 block text-sm font-medium">
            Name
            <input type="text" value={name}
              onInput={(e) => setName((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <label class="mb-4 block text-sm font-medium">
            Role
            <select value={role}
              onChange={(e) => setRole((e.target as HTMLSelectElement).value)} class={field}>
              {roles.map((r) => (
                <option key={r} value={r}>{r}</option>
              ))}
            </select>
          </label>
          <label class="mb-1 block text-sm font-medium">
            Password{editing && <span class="font-normal text-gray-400"> (leave blank to keep current)</span>}
            <input type="password" required={!editing} minLength={8} value={password}
              onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <p class="mb-5 text-xs text-gray-400">Minimum 8 characters</p>
          <button type="submit" disabled={busy}
            class="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">
            {busy ? "Saving…" : editing ? "Save" : "Create user"}
          </button>
        </form>
      </div>
    </div>
  );
}
