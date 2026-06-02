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
    <div class="mx-auto max-w-3xl px-4 py-8">
      <div class="mb-6 flex items-center justify-between">
        <h1 class="text-xl font-bold">Users</h1>
        <a href="/_/users/new"
          class="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">
          Add user
        </a>
      </div>

      {error && <div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{error}</div>}

      {users === null && !error ? (
        <p class="text-sm text-gray-400">Loading…</p>
      ) : users && users.length > 0 ? (
        <div class="overflow-x-auto rounded-lg bg-white shadow-sm">
          <table class="w-full">
            <thead>
              <tr class="border-b border-gray-200">
                <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Name</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Email</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Role</th>
                <th class="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id} class="border-b border-gray-100 hover:bg-gray-50">
                  <td class="px-4 py-3 text-sm font-medium">{u.name}</td>
                  <td class="px-4 py-3 text-sm text-gray-600">{u.email}</td>
                  <td class="px-4 py-3"><RoleBadge role={u.role} /></td>
                  <td class="px-4 py-3">
                    <div class="flex justify-end gap-2">
                      <a href={`/_/users/${encodeURIComponent(u.id)}/edit`}
                        class="rounded bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">
                        Edit
                      </a>
                      {u.id !== me.id && u.role !== "superadmin" && (
                        <button onClick={() => remove(u.id)}
                          class="rounded bg-red-600 px-2 py-1 text-xs font-medium text-white hover:bg-red-700">
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
        <div class="rounded-lg bg-white p-6 text-center shadow-sm">
          <p class="text-sm text-gray-500">No users yet.</p>
        </div>
      )}
    </div>
  );
}
