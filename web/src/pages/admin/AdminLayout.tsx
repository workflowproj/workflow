import { Link, Outlet } from "@tanstack/react-router";
import { useMe } from "@/api/queries";
import { EmptyState } from "@/components/ui";

const sections = [
  { label: "Users", to: "/admin/users" as const },
  { label: "Groups", to: "/admin/groups" as const },
  { label: "Roles", to: "/admin/roles" as const },
  { label: "Custom fields", to: "/admin/fields" as const },
  { label: "Sign-in & provisioning", to: "/admin/provisioning" as const },
  { label: "GitHub & GitLab", to: "/admin/scm" as const },
  { label: "Webhooks", to: "/admin/webhooks" as const },
  { label: "Audit log", to: "/admin/audit" as const },
];

export function AdminLayout() {
  const { data: me } = useMe();
  if (me && me.orgRole !== "admin") {
    return <EmptyState title="Administrators only" body="Organisation administration is limited to admins." />;
  }
  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-6 p-6 md:flex-row">
      <nav aria-label="Administration" className="flex shrink-0 gap-1 overflow-x-auto md:w-52 md:flex-col">
        <p className="hidden px-3 pb-2 text-[11px] font-semibold tracking-wide text-faint uppercase md:block">Administration</p>
        {sections.map((s) => (
          <Link key={s.to} to={s.to} className="rounded-md px-3 py-1.5 text-sm whitespace-nowrap text-muted hover:bg-surface-2 hover:text-ink" activeProps={{ className: "!bg-accent-soft !text-accent font-medium" }}>
            {s.label}
          </Link>
        ))}
      </nav>
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}
