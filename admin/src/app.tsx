import { useEffect, useState } from "preact/hooks";
import { LocationProvider, Router, Route } from "preact-iso";
import { api, ApiError, type User } from "./api";
import { AuthProvider } from "./auth";
import { Layout } from "./components/layout";
import { Login } from "./views/login";
import { Dashboard } from "./views/dashboard";
import { CollectionView } from "./views/collection";
import { RecordForm } from "./views/record-form";
import { NotFound } from "./views/not-found";

export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .me()
      .then((r) => setUser(r.user))
      .catch((e) => {
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

  const logout = async () => {
    await api.logout().catch(() => {});
    setUser(null);
  };

  return (
    <AuthProvider value={{ user, logout }}>
      <LocationProvider>
        <Layout>
          <Router>
            <Route path="/_/" component={Dashboard} />
            <Route path="/_/collections/:collection/new" component={RecordForm} />
            <Route path="/_/collections/:collection" component={CollectionView} />
            <Route path="/_/records/:id/edit" component={RecordForm} />
            <Route default component={NotFound} />
          </Router>
        </Layout>
      </LocationProvider>
    </AuthProvider>
  );
}
