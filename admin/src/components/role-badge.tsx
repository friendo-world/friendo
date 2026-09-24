import type { Role } from "../api";

// A small black badge with the role's name, the one place the admin uses a
// rounded corner.
export function RoleBadge({ role }: { role: Role }) {
  return (
    <span class="inline-block rounded-[5px] bg-ink px-1.5 py-0.5 text-[0.65rem] font-bold leading-tight text-white">
      {role}
    </span>
  );
}
