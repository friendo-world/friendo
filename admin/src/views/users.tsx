import { useEffect, useState } from "preact/hooks";
import { api, type User } from "../api";
import { useAuth } from "../auth";
import { RoleBadge } from "../components/role-badge";

export function Users() {
  const { user: me } = useAuth();
  const [users, setUsers] = useState<User[] | null>(null);
  const [error, setError] = useState("");

  function load() {
    setUsers(null);
    api
      .users()
      .then((r) => setUsers(r.users))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load users."));
  }

  useEffect(load, []);

  async function remove(id: string) {
    if (!confirm("Delete this user?")) return;
    try {
      await api.deleteUser(id);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Delete failed.");
    }
  }

  return (
    <div class="mx-auto max-w-[960px] px-4 py-8">
      <div class="mb-6 flex items-center justify-between">
        <h1 class="text-xl font-bold">Users</h1>
        <a href="/_/users/new"
          class="bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-link">
          Add user
        </a>
      </div>

      {error && <div class="mb-4 border border-crimson bg-tint px-3 py-2 text-sm text-crimson">{error}</div>}

      {users === null && !error ? (
        <p class="text-sm text-dim">Loading…</p>
      ) : users && users.length > 0 ? (
        <div class="overflow-x-auto bg-white border border-ink">
          <table class="w-full">
            <thead>
              <tr class="border-b border-ink">
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">Name</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">Email</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-dim">Role</th>
                <th class="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id} class="border-b border-ink hover:bg-tint">
                  <td class="px-4 py-3 text-sm font-bold">{u.name}</td>
                  <td class="px-4 py-3 text-sm text-dim">{u.email}</td>
                  <td class="px-4 py-3"><RoleBadge role={u.role} /></td>
                  <td class="px-4 py-3">
                    <div class="flex justify-end gap-2">
                      <a href={`/_/users/${encodeURIComponent(u.id)}/edit`}
                        class="bg-ink px-2 py-1 text-xs font-bold text-white hover:bg-link">
                        Edit
                      </a>
                      {u.id !== me.id && u.role !== "owner" && (
                        <button onClick={() => remove(u.id)}
                          class="bg-crimson px-2 py-1 text-xs font-bold text-white hover:bg-ink">
                          Delete
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div class="bg-white p-6 text-center border border-ink">
          <p class="text-sm text-dim">No users yet.</p>
        </div>
      )}
    </div>
  );
}
