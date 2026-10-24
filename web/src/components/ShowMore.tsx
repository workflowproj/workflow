import { useState } from "react";

/**
 * useShowMore renders a long list a page at a time: the first `page` items, then `page` more
 * each time. Thousands of rows (a big backlog or Kanban column) otherwise take seconds to draw.
 */
export function useShowMore(page: number) {
  const [limit, setLimit] = useState(page);
  return { limit, more: () => setLimit((n) => n + page) };
}

/** ShowMore is the button under a list cut at `limit`; it renders nothing when all of it shows. */
export function ShowMore({ total, limit, page, onMore, className }: { total: number; limit: number; page: number; onMore: () => void; className?: string }) {
  const hidden = total - limit;
  if (hidden <= 0) return null;
  return (
    <button type="button" onClick={onMore} className={className ?? "w-full px-3 py-2 text-[13px] font-medium text-accent hover:bg-surface-2/60 focus-visible:outline-2 focus-visible:outline-accent"}>
      Show {Math.min(page, hidden).toLocaleString()} more <span className="font-normal text-muted">({hidden.toLocaleString()} not shown)</span>
    </button>
  );
}
