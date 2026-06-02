import type { User } from "../api";
import { Nav } from "../components/nav";

// Phase 1 placeholder shell. Records / users / settings views land in later
// phases; for now this proves the authenticated bundle renders on both runtimes.
export function Shell({ user, onLogout }: { user: User; onLogout: () => void }) {
  return (
    <div>
      <Nav user={user} active="dashboard" onLogout={onLogout} />
      <div class="mx-auto max-w-3xl px-4 py-8">
        <h1 class="text-xl font-bold">Welcome back, {user.name}</h1>
        <p class="mt-1 text-sm text-gray-500">
          Signed in as {user.email} · {user.role}
        </p>
        <p class="mt-6 text-sm text-gray-400">
          The admin UI is being rebuilt as a shared single-page app. Records, users,
          and settings are coming next.
        </p>
      </div>
    </div>
  );
}
