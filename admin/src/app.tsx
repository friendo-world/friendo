import { useEffect, useState } from "preact/hooks";
import { LocationProvider, Router, Route } from "preact-iso";
import { ALL_FEATURES_ON, api, ApiError, type Features, type SetupStatus, type User } from "./api";
import { AuthProvider } from "./auth";
import { Layout } from "./components/layout";
import { ToastProvider } from "./components/toast";
import { Login } from "./views/login";
import { Setup } from "./views/setup";
import { Migrate } from "./views/migrate";
import { CollectionsArea } from "./views/collections";
import { RecordRedirect } from "./views/record-redirect";
import { Users } from "./views/users";
import { UserForm } from "./views/user-form";
import { SettingsView } from "./views/settings";
import { ModerationPage } from "./views/moderation-page";
import { Attendees } from "./views/attendees";
import { NotFound } from "./views/not-found";

const NO_SETUP: SetupStatus = { needsSetup: false, hasLegacyAdmin: false };

export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [setup, setSetup] = useState<SetupStatus>(NO_SETUP);
  const [loading, setLoading] = useState(true);
  const [features, setFeatures] = useState<Features>(ALL_FEATURES_ON);
  const reloadFeatures = () => {
    api.features().then((r) => setFeatures({ ...ALL_FEATURES_ON, ...r.features })).catch(() => {});
  };

  useEffect(reloadFeatures, []);

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
      <div class="flex min-h-screen items-center justify-center text-sm text-dim">
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
    <AuthProvider value={{ user, logout, features, reloadFeatures }}>
      <LocationProvider>
        <ToastProvider>
        <Layout>
          <Router>
            <Route path="/_/" component={CollectionsArea} />
            <Route path="/_/collections/:collection/:id?" component={CollectionsArea} />
            <Route path="/_/records/:id/edit" component={RecordRedirect} />
            <Route path="/_/records/:id/attendees" component={Attendees} />
            <Route path="/_/users" component={Users} />
            <Route path="/_/users/new" component={UserForm} />
            <Route path="/_/users/:id/edit" component={UserForm} />
            <Route path="/_/moderation" component={ModerationPage} />
            <Route path="/_/review" component={ModerationPage} />
            <Route path="/_/settings" component={SettingsView} />
            <Route default component={NotFound} />
          </Router>
        </Layout>
        </ToastProvider>
      </LocationProvider>
    </AuthProvider>
  );
}
