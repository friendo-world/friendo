import type { Role } from "../api";

// Literal classes so Tailwind picks them up when scanning source.
const COLORS: Record<Role, string> = {
  owner: "bg-purple-600",
  admin: "bg-blue-600",
  editor: "bg-emerald-600",
  contributor: "bg-teal-600",
  member: "bg-gray-500",
};

export function RoleBadge({ role }: { role: Role }) {
  return (
    <span
      class={`inline-block rounded-full px-2 py-0.5 text-xs font-semibold text-white ${COLORS[role] || "bg-gray-500"}`}
    >
      {role}
    </span>
  );
}
