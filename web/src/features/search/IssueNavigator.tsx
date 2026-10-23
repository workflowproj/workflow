import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Code2, Filter, Rows3, Save, Search, TriangleAlert } from "lucide-react";
import { ApiError } from "@/api/client";
import { Button, Popover } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { cn } from "@/lib/cn";
import { BasicFilters } from "./BasicFilters";
import { JqlEditor, type JqlError } from "./JqlEditor";
import { buildJql, parseBasic, runeOffsetToIndex, splitOrderBy, toggleSort, type BasicFilters as Filters } from "./jql";
import { ResultsTable } from "./ResultsTable";
import { FiltersPanel, SaveFilterDialog, sameJql, type SavedFilter } from "./SavedFilters";
import { filtersQuery, searchResultsQuery } from "./search-api";

export type NavigatorSearch = { jql?: string; filter?: string; issue?: string };

type Mode = "basic" | "jql";
const MODE_KEY = "wfm.search.mode";

function useMode(): [Mode, (m: Mode) => void] {
  const [mode, setMode] = useState<Mode>(() => {
    try {
      return localStorage.getItem(MODE_KEY) === "jql" ? "jql" : "basic";
    } catch {
      return "basic";
    }
  });
  const set = (m: Mode) => {
    setMode(m);
    try {
      localStorage.setItem(MODE_KEY, m);
    } catch {
      // Private windows may refuse storage; the mode just will not stick.
    }
  };
  return [mode, set];
}

/**
 * IssueNavigator is the issue search: basic filters or a JQL editor, sortable results, saved
 * filters and the issue drawer. Its state lives in the URL (`jql`, `filter`, `issue`) so
 * searches can be shared; `onSearch` writes it back. `projectKey` scopes the default query.
 */
export function IssueNavigator({
  projectKey,
  search,
  onSearch,
  title,
}: {
  projectKey?: string;
  search: NavigatorSearch;
  onSearch: (next: NavigatorSearch) => void;
  title?: string;
}) {
  const filters = useQuery(filtersQuery);
  const filterById = search.filter ? filters.data?.find((f) => f.id === search.filter) : undefined;
  const defaultJql = projectKey ? `project = ${projectKey} ORDER BY updated DESC` : "ORDER BY updated DESC";
  const waitingForFilter = !!search.filter && !search.jql && filters.isLoading;
  const jql = search.jql ?? filterById?.jql ?? defaultJql;
  // The filter in effect: the one in the URL, or a saved filter whose query is on screen.
  const active = filterById ?? (search.filter ? undefined : filters.data?.find((f) => sameJql(f.jql, jql)));
  const modified = !!active && !sameJql(active.jql, jql);

  const results = useInfiniteQuery({ ...searchResultsQuery(jql), enabled: !waitingForFilter });
  const issues = useMemo(() => results.data?.pages.flatMap((p) => p.issues) ?? [], [results.data]);
  const total = results.data?.pages[0]?.total ?? 0;

  const [draft, setDraft] = useState(jql);
  useEffect(() => setDraft(jql), [jql]);
  const [mode, setMode] = useMode();
  const parsed = useMemo(() => parseBasic(jql, projectKey), [jql, projectKey]);
  const [saving, setSaving] = useState<"new" | "update" | null>(null);

  const run = (next: string) => {
    const q = next.trim();
    const keepFilter = search.filter && filterById && sameJql(filterById.jql, q);
    onSearch({
      filter: search.filter,
      jql: keepFilter || (!search.filter && (q === "" || sameJql(q, defaultJql))) ? undefined : q || defaultJql,
      issue: search.issue,
    });
  };
  const openFilter = (f: SavedFilter) => onSearch(projectKey ? { jql: f.jql } : { filter: f.id });
  const openIssue = (key?: string) => onSearch({ ...search, issue: key });

  const queryError = results.error instanceof ApiError && results.error.status === 400 ? results.error : null;
  const jqlError: JqlError | null =
    queryError && sameJql(draft, jql)
      ? {
          message: queryError.problem.detail ?? queryError.message,
          index: queryError.problem.position !== undefined ? runeOffsetToIndex(jql, queryError.problem.position) : undefined,
        }
      : null;

  const panel = (
    <FiltersPanel
      projectKey={projectKey}
      currentJql={jql}
      activeId={active?.id}
      onPreset={(q) => onSearch({ jql: sameJql(q, defaultJql) ? undefined : q, issue: search.issue })}
      onFilter={openFilter}
      onDeleted={(f) => f.id === active?.id && onSearch({ issue: search.issue })}
    />
  );

  return (
    <div className="flex h-full min-h-0">
      <aside className="hidden w-56 shrink-0 overflow-y-auto border-r border-line bg-surface px-2 py-4 2xl:block">{panel}</aside>
      <div className="min-w-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[1400px] flex-col gap-4 p-4 sm:p-6">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex min-w-0 items-center gap-2">
              <div className="2xl:hidden">
                <Popover
                  trigger={({ toggle }) => (
                    <Button size="sm" icon={<Filter className="size-3.5" />} onClick={toggle}>
                      Filters
                    </Button>
                  )}
                  content={() => <div className="w-64 py-2">{panel}</div>}
                />
              </div>
              <div className="min-w-0">
                <h1 className="truncate text-lg font-semibold tracking-tight">
                  {active?.name ?? title ?? "Issues"}
                  {modified && <span className="ml-2 align-middle text-xs font-medium text-warning">Edited</span>}
                </h1>
                {active && (
                  <p className="truncate text-[13px] text-muted">
                    {active.description || (active.mine ? (active.shared ? "Your filter · shared with everyone" : "Your filter") : `Shared by ${active.owner.displayName}`)}
                  </p>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2">
              {active?.mine && modified && (
                <Button size="sm" variant="primary" icon={<Save className="size-3.5" />} onClick={() => setSaving("update")} disabled={!!queryError}>
                  Save changes
                </Button>
              )}
              <Button size="sm" icon={<Save className="size-3.5" />} onClick={() => setSaving("new")} disabled={!!queryError}>
                {active ? "Save as new filter" : "Save filter"}
              </Button>
            </div>
          </div>

          <div className="flex flex-col gap-2.5">
            <div className="flex items-start gap-2">
              <ModeSwitch mode={mode} onChange={setMode} />
              <div className="min-w-0 flex-1">
                {mode === "basic" ? (
                  parsed ? (
                    <BasicFilters
                      value={parsed.filters}
                      projectKey={projectKey}
                      onChange={(f: Filters) => run(buildJql(f, parsed.orderBy || splitOrderBy(defaultJql).orderBy, projectKey))}
                    />
                  ) : (
                    <div className="flex flex-wrap items-center gap-2 rounded-md border border-warning/40 bg-warning-soft px-3 py-1.5 text-[13px] text-warning">
                      <TriangleAlert className="size-4 shrink-0" />
                      <span className="flex-1">This query uses conditions the basic filters can't show.</span>
                      <Button size="sm" variant="ghost" onClick={() => setMode("jql")}>
                        Edit as JQL
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => run(defaultJql)}>
                        Start over
                      </Button>
                    </div>
                  )
                ) : (
                  <JqlEditor value={draft} onChange={setDraft} onRun={run} error={jqlError} projectKey={projectKey} />
                )}
              </div>
              {mode === "jql" && (
                <Button variant="primary" icon={<Search className="size-4" />} onClick={() => run(draft)}>
                  Search
                </Button>
              )}
            </div>
            {mode === "basic" && parsed && (
              <p className="truncate font-mono text-[11px] text-faint" title={jql}>
                {jql}
              </p>
            )}
          </div>

          {results.error && !jqlError && !(mode === "jql" && queryError) && <ErrorBox error={results.error} />}
          {queryError && mode === "jql" ? (
            <div className="rounded-lg border border-dashed border-line-strong px-4 py-10 text-center text-sm text-muted">Fix the query and press Enter to see results.</div>
          ) : (
            <ResultsTable
              jql={jql}
              issues={issues}
              total={total}
              selectedKey={search.issue}
              loading={results.isLoading || waitingForFilter}
              dimmed={results.isFetching && !results.isFetchingNextPage && !results.isLoading}
              hasMore={!!results.hasNextPage}
              loadingMore={results.isFetchingNextPage}
              onOpen={openIssue}
              onSort={(field, desc) => run(toggleSort(jql, field, desc))}
              onLoadMore={() => results.fetchNextPage()}
            />
          )}
        </div>
      </div>
      <IssueDrawer issueKey={search.issue} onClose={() => openIssue(undefined)} onNavigate={(key) => openIssue(key)} />
      <SaveFilterDialog
        open={saving !== null}
        onClose={() => setSaving(null)}
        jql={jql}
        filter={saving === "update" ? active : undefined}
        onSaved={(f) => onSearch(projectKey ? { jql: f.jql, issue: search.issue } : { filter: f.id, issue: search.issue })}
      />
    </div>
  );
}

function ModeSwitch({ mode, onChange }: { mode: Mode; onChange: (m: Mode) => void }) {
  const opts = [
    { id: "basic" as const, label: "Basic", icon: Rows3 },
    { id: "jql" as const, label: "JQL", icon: Code2 },
  ];
  return (
    <div role="radiogroup" aria-label="Search mode" className="inline-flex h-9 shrink-0 rounded-md border border-line-strong bg-surface-2 p-0.5">
      {opts.map((o) => (
        <button
          key={o.id}
          type="button"
          role="radio"
          aria-checked={mode === o.id}
          onClick={() => onChange(o.id)}
          className={cn("inline-flex items-center gap-1.5 rounded px-2.5 text-[13px]", mode === o.id ? "bg-surface font-medium text-ink shadow-sm" : "text-muted hover:text-ink")}
        >
          <o.icon className="size-3.5" />
          {o.label}
        </button>
      ))}
    </div>
  );
}
