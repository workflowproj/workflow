import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeftRight, LifeBuoy, LogOut, UserRound } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { useMe } from "@/api/queries";
import { MenuItem, Popover } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { ThemeMenu } from "@/layout/ThemeMenu";
import { portalApprovalsQuery } from "@/features/servicedesk/api";

/**
 * PortalLayout is the customer-facing help centre: its own header and navigation, none of the
 * agent app's. Customers only ever see these pages; agents and other members can use them too,
 * to raise and follow their own requests.
 */
export function PortalLayout() {
  const { data: me } = useMe();
  const { data: approvals = [] } = useQuery(portalApprovalsQuery);
  const nav = "rounded-md px-2.5 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-ink";
  const active = { className: "!bg-accent-soft !text-accent font-medium" };
  return (
    <div className="flex min-h-full flex-col">
      <a
        href="#portal-main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded focus:bg-surface focus:px-3 focus:py-1.5 focus:shadow"
      >
        Skip to content
      </a>
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-3 px-4 sm:px-6">
          <Link to="/portal" className="flex items-center gap-2 font-semibold tracking-tight">
            <span className="flex size-7 items-center justify-center rounded-md bg-accent text-accent-ink">
              <LifeBuoy aria-hidden className="size-4" />
            </span>
            <span>Help centre</span>
          </Link>
          <nav aria-label="Help centre" className="ml-2 hidden items-center gap-1 sm:flex">
            <Link to="/portal" className={nav} activeProps={active} activeOptions={{ exact: true }}>
              Get help
            </Link>
            <Link to="/portal/requests" className={nav} activeProps={active}>
              My requests
            </Link>
            <Link to="/portal/approvals" className={nav} activeProps={active}>
              Approvals
              {approvals.length > 0 && (
                <span className="ml-1.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-bold text-accent-ink">
                  <span className="sr-only">(</span>
                  {approvals.length}
                  <span className="sr-only"> waiting)</span>
                </span>
              )}
            </Link>
          </nav>
          <div className="ml-auto flex items-center gap-1.5">
            <ThemeMenu />
            {me && <AccountMenu />}
          </div>
        </div>
        {/* Small screens: the navigation as a row of links under the bar. */}
        <nav aria-label="Help centre (compact)" className="flex gap-1 overflow-x-auto border-t border-line px-3 py-1.5 sm:hidden">
          <Link to="/portal" className={nav} activeProps={active} activeOptions={{ exact: true }}>
            Get help
          </Link>
          <Link to="/portal/requests" className={nav} activeProps={active}>
            My requests
          </Link>
          <Link to="/portal/approvals" className={nav} activeProps={active}>
            Approvals{approvals.length > 0 ? ` (${approvals.length})` : ""}
          </Link>
        </nav>
      </header>
      <main id="portal-main" className="mx-auto w-full max-w-5xl flex-1 px-4 py-6 sm:px-6 sm:py-8">
        <Outlet />
      </main>
    </div>
  );
}

function AccountMenu() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const logout = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/logout")),
    onSettled: () => {
      qc.clear();
      navigate({ to: "/login", search: { returnTo: "/portal" } });
    },
  });
  if (!me) return null;
  return (
    <Popover
      align="end"
      trigger={({ toggle }) => (
        <button onClick={toggle} aria-label="Account menu" className="rounded-full p-0.5 hover:bg-surface-2">
          <UserAvatar user={me} size={28} />
        </button>
      )}
      content={(close) => (
        <div className="w-60">
          <div className="border-b border-line px-2.5 pt-1.5 pb-2.5">
            <p className="truncate text-sm font-medium">{me.displayName}</p>
            <p className="truncate text-xs text-muted">{me.email}</p>
          </div>
          <div className="pt-1">
            <MenuItem icon={<UserRound className="size-4" />} onClick={() => (close(), navigate({ to: "/portal/profile" }))}>
              Profile
            </MenuItem>
            {me.orgRole !== "customer" && (
              <MenuItem icon={<ArrowLeftRight className="size-4" />} onClick={() => (close(), navigate({ to: "/projects" }))}>
                Back to Workflow
              </MenuItem>
            )}
            <MenuItem icon={<LogOut className="size-4" />} onClick={() => logout.mutate()}>
              Sign out
            </MenuItem>
          </div>
        </div>
      )}
    />
  );
}
