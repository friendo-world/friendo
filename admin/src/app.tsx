import { useEffect, useState } from "preact/hooks";
import { api, ApiError, type User } from "./api";
import { Login } from "./views/login";
import { Shell } from "./views/shell";

export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .me()
      .then((r) => setUser(r.user))
      .catch((e) => {
        // 401 simply means "not logged in" — anything else is unexpected.
        if (!(e instanceof ApiError) || e.status !== 401) console.error(e);
        setUser(null);
      })
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div class="flex min-h-screen items-center justify-center text-sm text-gray-400">
        Loading…
      </div>
    );
  }

  if (!user) return <Login onLogin={setUser} />;

  return (
    <Shell
      user={user}
      onLogout={async () => {
        await api.logout().catch(() => {});
        setUser(null);
      }}
    />
  );
}
