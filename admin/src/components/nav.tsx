import type { User } from "../api";

type Tab = "dashboard" | "users" | "settings";

export function Nav({
  user,
  active,
  onLogout,
}: {
  user: User;
  active: Tab | "";
  onLogout: () => void;
}) {
  const link = (href: string, label: string, tab: Tab) => (
    <a
      href={href}
      class={
        "text-sm " +
        (active === tab ? "font-medium text-blue-600" : "text-gray-500 hover:text-gray-900")
      }
    >
      {label}
    </a>
  );

  return (
    <nav class="flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-gray-200 bg-white px-4 py-3 sm:px-6">
      <span class="font-bold text-gray-900">Friendo</span>
      {link("/_/", "Dashboard", "dashboard")}
      {link("/_/users", "Users", "users")}
      {link("/_/settings", "Settings", "settings")}
      <a href="/" class="text-sm text-gray-500 hover:text-gray-900">
        View site
      </a>
      <div class="ml-auto flex items-center gap-3">
        <span class="hidden text-xs text-gray-400 sm:inline">{user.email}</span>
        <button
          onClick={onLogout}
          class="rounded border border-gray-300 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50"
        >
          Log out
        </button>
      </div>
    </nav>
  );
}
