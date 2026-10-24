import { Link, Outlet } from "@tanstack/react-router";
import { FolderGit2, KeyRound, Mail, Plug, ScrollText, ShieldCheck, TextCursorInput, UserRound, UsersRound, Webhook } from "lucide-react";
import { useMe } from "@/api/queries";
import { EmptyState } from "@/components/ui";

const sections = [
  { label: "Users", to: "/admin/users" as const, icon: UserRound },
  { label: "Groups", to: "/admin/groups" as const, icon: UsersRound },
  { label: "Roles", to: "/admin/roles" as const, icon: ShieldCheck },
  { label: "Custom fields", to: "/admin/fields" as const, icon: TextCursorInput },
  { label: "Sign-in & provisioning", to: "/admin/provisioning" as const, icon: KeyRound },
  { label: "GitHub & GitLab", to: "/admin/scm" as const, icon: FolderGit2 },
  { label: "Webhooks", to: "/admin/webhooks" as const, icon: Webhook },
  { label: "Email", to: "/admin/email" as const, icon: Mail },
  { label: "Connected apps", to: "/admin/connected-apps" as const, icon: Plug },
  { label: "Audit log", to: "/admin/audit" as const, icon: ScrollText },
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
          <Link
            key={s.to}
            to={s.to}
            className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm whitespace-nowrap text-muted hover:bg-surface-2 hover:text-ink"
            activeProps={{ className: "!bg-accent-soft !text-accent font-medium" }}
          >
            <s.icon className="size-4 shrink-0" aria-hidden />
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
