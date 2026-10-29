import { useEffect, useRef, useState } from "react";
import { Link, Outlet, useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, BookOpen, CalendarClock, FolderKanban, GanttChart, LayoutDashboard, LifeBuoy, LogOut, Menu, Plus, Search, Settings, Shield, UserRound } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { projectsQuery, useMe } from "@/api/queries";
import { Button, IconButton, MenuItem, Popover } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { portalDesksQuery } from "@/features/servicedesk/api";
import { ThemeMenu } from "./ThemeMenu";
import { cn } from "@/lib/cn";
import { projectHome } from "@/lib/project";

const ISSUE_KEY = /^[A-Za-z][A-Za-z0-9_]{1,9}-\d+$/;

export function AppShell() {
  const [navOpen, setNavOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const params = useParams({ strict: false }) as { projectKey?: string };
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  useEffect(() => setNavOpen(false), [pathname]);

  // Keyboard shortcuts: "c" creates an issue, "/" focuses search (outside text fields).
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.metaKey || e.ctrlKey || e.altKey || t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)) return;
      if (document.querySelector("[aria-modal=true]")) return;
      if (e.key === "c") {
        e.preventDefault();
        setCreating(true);
      } else if (e.key === "/") {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="flex h-full">
      <Sidebar className="hidden md:flex" />
      {navOpen && (
        <div className="fixed inset-0 z-40 flex md:hidden" onClick={() => setNavOpen(false)}>
          <div className="absolute inset-0 bg-black/30" />
          <Sidebar className="relative z-10 flex" />
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b border-line bg-surface px-3 md:px-4">
          <IconButton label="Open navigation" className="md:hidden" onClick={() => setNavOpen(true)}>
            <Menu className="size-4.5" />
          </IconButton>
          <QuickSearch inputRef={searchRef} />
          <div className="ml-auto flex items-center gap-1.5">
            <Button variant="primary" size="sm" icon={<Plus className="size-3.5" />} onClick={() => setCreating(true)} title="Create issue (c)">
              Create
            </Button>
            <ThemeMenu />
            <NotificationBell />
            <UserMenu />
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
      <CreateIssueDialog open={creating} onClose={() => setCreating(false)} projectKey={params.projectKey} />
      <VersionNote />
    </div>
  );
}

const RELEASE = /^\d+\.\d+\.\d+$/;

/** VersionNote shows the server's version in the bottom-right corner; releases link to their notes. */
export function VersionNote() {
  const { data: me } = useMe();
  const version = me?.version;
  if (!version) return null;
  const label = RELEASE.test(version) ? `v${version}` : version;
  return (
    // Clicks pass through to the page except on the link; toasts (z-60) sit above it.
    <div className="pointer-events-none fixed right-3 bottom-2 z-10 hidden text-[11px] text-faint select-none md:block">
      {RELEASE.test(version) ? (
        <a
          href={`https://github.com/workflowproj/workflow/releases/tag/v${version}`}
          target="_blank"
          rel="noreferrer"
          className="pointer-events-auto rounded px-1 hover:text-muted hover:underline"
          title="Release notes"
        >
          Workflow {label}
        </a>
      ) : (
        <span>Workflow {label}</span>
      )}
    </div>
  );
}

function Sidebar({ className }: { className?: string }) {
  const { data: me } = useMe();
  const { data: projects = [] } = useQuery(projectsQuery);
  const { data: desks = [] } = useQuery({ ...portalDesksQuery, staleTime: 5 * 60_000 });
  const params = useParams({ strict: false }) as { projectKey?: string };
  const navItem = "flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-ink";
  const active = { className: "!bg-accent-soft !text-accent font-medium" };
  return (
    <aside className={cn("w-60 shrink-0 flex-col border-r border-line bg-surface", className)} onClick={(e) => e.stopPropagation()}>
      <Link to="/projects" className="flex h-12 items-center gap-2 border-b border-line px-4 font-semibold tracking-tight">
        <span className="flex size-6 items-center justify-center rounded-md bg-accent text-accent-ink">
          <FolderKanban className="size-3.5" />
        </span>
        Workflow
      </Link>
      <nav className="flex flex-col gap-0.5 p-2">
        <Link to="/projects" className={navItem} activeProps={active} activeOptions={{ exact: true }}>
          <FolderKanban className="size-4" /> Projects
        </Link>
        <Link to="/issues" className={navItem} activeProps={active}>
          <Search className="size-4" /> Issues
        </Link>
        <Link to="/timeline" className={navItem} activeProps={active}>
          <GanttChart className="size-4" /> Timeline
        </Link>
        <Link to="/dashboards" className={navItem} activeProps={active}>
          <LayoutDashboard className="size-4" /> Dashboards
        </Link>
        <Link to="/timesheet" className={navItem} activeProps={active}>
          <CalendarClock className="size-4" /> Timesheet
        </Link>
        <Link to="/notifications" className={navItem} activeProps={active}>
          <Bell className="size-4" /> Notifications
        </Link>
        {desks.length > 0 && (
          <Link to="/portal" className={navItem}>
            <LifeBuoy className="size-4" /> Help centre
          </Link>
        )}
      </nav>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        <p className="px-2.5 pt-3 pb-1.5 text-[11px] font-semibold tracking-wide text-faint uppercase">Projects</p>
        {projects.length === 0 && <p className="px-2.5 text-[13px] text-muted">No projects yet</p>}
        {projects.map((p) => (
          <Link
            key={p.id}
            to={projectHome(p)}
            params={{ projectKey: p.key }}
            className={cn(navItem, "py-1", params.projectKey === p.key && "bg-surface-2 text-ink")}
          >
            <ProjectAvatar projectKey={p.key} />
            <span className="truncate">{p.name}</span>
          </Link>
        ))}
      </div>
      {me?.orgRole === "admin" && (
        <div className="border-t border-line p-2">
          <Link to="/admin" className={navItem} activeProps={active}>
            <Shield className="size-4" /> Administration
          </Link>
        </div>
      )}
    </aside>
  );
}

// White initials stay readable (WCAG AA 4.5:1) on every colour.
const projectColors = ["#2563eb", "#7c3aed", "#0f766e", "#db2777", "#c2410c", "#4f46e5", "#0e7490", "#4d7c0f"];

export function ProjectAvatar({ projectKey, size = 20 }: { projectKey: string; size?: number }) {
  let h = 0;
  for (const c of projectKey) h = (h * 33 + c.charCodeAt(0)) | 0;
  return (
    <span
      aria-hidden
      style={{ width: size, height: size, background: projectColors[Math.abs(h) % projectColors.length], fontSize: Math.round(size * 0.42) }}
      className="inline-flex shrink-0 items-center justify-center rounded-[5px] font-bold text-white"
    >
      {projectKey.slice(0, 2)}
    </span>
  );
}

function QuickSearch({ inputRef }: { inputRef: React.RefObject<HTMLInputElement | null> }) {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const text = q.trim();
    if (!text) return;
    if (ISSUE_KEY.test(text)) navigate({ to: "/browse/$issueKey", params: { issueKey: text.toUpperCase() } });
    else navigate({ to: "/issues", search: { jql: `text ~ "${text.replaceAll('"', '\\"')}" ORDER BY updated DESC` } });
    setQ("");
    inputRef.current?.blur();
  };
  return (
    <form onSubmit={submit} role="search" className="relative w-full max-w-sm">
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-faint" />
      <input
        ref={inputRef}
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search issues or jump to a key"
        aria-label="Search issues"
        className="h-8 w-full rounded-md border border-line bg-surface-2 pr-8 pl-8 text-sm placeholder:text-faint focus:border-accent focus:bg-surface focus:outline-none"
      />
      <kbd className="pointer-events-none absolute top-1/2 right-2 hidden -translate-y-1/2 rounded border border-line px-1 text-[10px] text-faint sm:block">/</kbd>
    </form>
  );
}

function NotificationBell() {
  const { data } = useQuery({
    queryKey: ["notifications", "count"],
    queryFn: () => unwrap(api.GET("/notifications", { params: { query: { unread: true, limit: 1 } } })),
    refetchInterval: 60_000,
  });
  const unread = data?.unread ?? 0;
  return (
    <Link to="/notifications" aria-label={unread ? `Notifications (${unread} unread)` : "Notifications"} className="relative inline-flex size-8.5 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-ink">
      <Bell className="size-4.5" />
      {unread > 0 && (
        <span className="absolute top-1 right-1 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-bold text-danger-ink">
          {unread > 99 ? "99+" : unread}
        </span>
      )}
    </Link>
  );
}

function UserMenu() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const logout = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/logout")),
    onSettled: (data) => {
      qc.clear();
      // With single logout the sign-in provider ends its session too, then returns to /login.
      if (data?.redirectUrl) window.location.assign(data.redirectUrl);
      else navigate({ to: "/login" });
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
            <MenuItem icon={<UserRound className="size-4" />} onClick={() => (close(), navigate({ to: "/profile" }))}>
              Profile and tokens
            </MenuItem>
            <MenuItem icon={<BookOpen className="size-4" />} onClick={() => (close(), navigate({ to: "/api-docs" }))}>
              API docs
            </MenuItem>
            {me.orgRole === "admin" && (
              <MenuItem icon={<Settings className="size-4" />} onClick={() => (close(), navigate({ to: "/admin" }))}>
                Administration
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

