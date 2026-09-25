import type { ComponentChildren } from "preact";
import { useLocation } from "preact-iso";
import { useAuth } from "../auth";
import { Nav } from "./nav";

export function Layout({ children }: { children: ComponentChildren }) {
  const { user, logout } = useAuth();
  // The router may give the root as "/_" or "/_/"; compare without the slash.
  const path = useLocation().path.replace(/\/$/, "");

  const active =
    path === "/_" || path.startsWith("/_/collections")
      ? "content"
      : path.startsWith("/_/moderation") || path.startsWith("/_/review")
        ? "moderation"
        : path.startsWith("/_/users")
          ? "users"
          : path.startsWith("/_/settings")
            ? "settings"
            : "";

  return (
    <div class="flex min-h-screen flex-col">
      <Nav user={user} active={active} onLogout={logout} />
      <div class="flex-1">{children}</div>
      <footer class="mx-auto w-full max-w-[1200px] px-4 py-4 text-xs text-dim">
        <a href="https://docs.friendo.world" target="_blank" rel="noopener" class="text-link hover:underline">
          friendo docs
        </a>
        <span class="mx-2">·</span>
        <a href="https://friendo.world" target="_blank" rel="noopener" class="hover:underline">
          friendo.world
        </a>
      </footer>
    </div>
  );
}
