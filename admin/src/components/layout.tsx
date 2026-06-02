import type { ComponentChildren } from "preact";
import { useLocation } from "preact-iso";
import { useAuth } from "../auth";
import { Nav } from "./nav";

export function Layout({ children }: { children: ComponentChildren }) {
  const { user, logout } = useAuth();
  const { path } = useLocation();

  const active = path === "/_/"
    ? "dashboard"
    : path.startsWith("/_/users")
      ? "users"
      : path.startsWith("/_/settings")
        ? "settings"
        : "";

  return (
    <div>
      <Nav user={user} active={active} onLogout={logout} />
      {children}
    </div>
  );
}
