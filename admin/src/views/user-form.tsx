import { useEffect, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { api, availableRoles, type UserInput } from "../api";
import { useAuth } from "../auth";

const field =
  "mt-1 block w-full border border-ink px-3 py-2 text-sm focus:border-link focus:ring-1 focus:ring-link focus:outline-none";

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
  // The password field only appears when the site allows password sign-in;
  // otherwise an account is complete with just an email (they sign in with a code).
  const [passwordLogin, setPasswordLogin] = useState(false);

  useEffect(() => {
    api.settings().then((s) => setPasswordLogin(s.access.password_login)).catch(() => {});
  }, []);

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
        : { email, name, role, ...(password ? { password } : {}) };
      if (editing) await api.updateUser(id!, input);
      else await api.createUser(input);
      route("/_/users");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save failed.");
      setBusy(false);
    }
  }

  if (loading) {
    return <div class="mx-auto max-w-[960px] px-4 py-8 text-sm text-dim">Loading…</div>;
  }

  return (
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <h1 class="mb-6 text-xl font-bold">{editing ? "Edit user" : "Add user"}</h1>
      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}
      <div class="bg-white p-6 border border-ink">
        <form onSubmit={submit}>
          {editing ? (
            <div class="mb-4 text-sm">
              <span class="font-bold text-dim">Email:</span> <span>{email}</span>
            </div>
          ) : (
            <label class="mb-4 block text-sm font-bold">
              Email
              <input type="email" required value={email}
                onInput={(e) => setEmail((e.target as HTMLInputElement).value)} class={field} />
            </label>
          )}
          <label class="mb-4 block text-sm font-bold">
            Name
            <input type="text" value={name}
              onInput={(e) => setName((e.target as HTMLInputElement).value)} class={field} />
          </label>
          <label class="mb-4 block text-sm font-bold">
            Role
            <select value={role}
              onChange={(e) => setRole((e.target as HTMLSelectElement).value)} class={field}>
              {roles.map((r) => (
                <option key={r} value={r}>{r}</option>
              ))}
            </select>
          </label>
          {passwordLogin ? (
            <>
              <label class="mb-1 block text-sm font-bold">
                Password <span class="font-normal text-dim">(optional{editing ? " — leave blank to keep current" : ""})</span>
                <input type="password" minLength={8} value={password}
                  onInput={(e) => setPassword((e.target as HTMLInputElement).value)} class={field} />
              </label>
              <p class="mb-5 text-xs text-dim">
                Minimum 8 characters. They can always sign in with a code sent to their email instead.
              </p>
            </>
          ) : (
            <p class="mb-5 text-xs text-dim">
              They'll sign in with a code sent to their email. (Turn on password sign-in in Settings to set passwords.)
            </p>
          )}
          <button type="submit" disabled={busy}
            class="bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link disabled:opacity-50">
            {busy ? "Saving…" : editing ? "Save" : "Create user"}
          </button>
        </form>
      </div>
    </div>
  );
}
