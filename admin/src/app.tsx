import { useEffect, useState } from "preact/hooks";
import { LocationProvider, Router, Route } from "preact-iso";
import { api, ApiError, type SetupStatus, type User } from "./api";
import { AuthProvider } from "./auth";
import { Layout } from "./components/layout";
import { Login } from "./views/login";
import { Setup } from "./views/setup";
import { Migrate } from "./views/migrate";
import { Dashboard } from "./views/dashboard";
import { CollectionView } from "./views/collection";
import { RecordForm } from "./views/record-form";
import { Users } from "./views/users";
import { UserForm } from "./views/user-form";
import { SettingsView } from "./views/settings";
import { Moderation } from "./views/moderation";
import { Review } from "./views/review";
import { NotFound } from "./views/not-found";

const NO_SETUP: SetupStatus = { needsSetup: false, hasLegacyAdmin: false };

export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [setup, setSetup] = useState<SetupStatus>(NO_SETUP);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .me()
      .then((r) => setUser(r.user))
      .catch(async (e) => {
        if (e instanceof ApiError && e.status === 401) {
          // Not logged in — find out whether this is a first-run site.
          setSetup(await api.setupStatus().catch(() => NO_SETUP));
        } else {
          console.error(e);
        }
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

  if (!user) {
    if (setup.needsSetup && setup.hasLegacyAdmin) {
      return <Migrate onDone={() => setSetup(NO_SETUP)} />;
    }
    if (setup.needsSetup) return <Setup status={setup} onLogin={setUser} />;
    return <Login status={setup} onLogin={setUser} />;
  }

  const logout = async () => {
    await api.logout().catch(() => {});
    setUser(null);
    setSetup(await api.setupStatus().catch(() => NO_SETUP));
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
            <Route path="/_/users" component={Users} />
            <Route path="/_/users/new" component={UserForm} />
            <Route path="/_/users/:id/edit" component={UserForm} />
            <Route path="/_/review" component={Review} />
            <Route path="/_/moderation" component={Moderation} />
            <Route path="/_/settings" component={SettingsView} />
            <Route default component={NotFound} />
          </Router>
        </Layout>
      </LocationProvider>
    </AuthProvider>
  );
}
