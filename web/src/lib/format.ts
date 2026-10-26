const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
const dateFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });
const dateTimeFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** relativeTime renders "3 minutes ago" style text. */
export function relativeTime(iso: string | undefined | null): string {
  if (!iso) return "";
  const diff = (new Date(iso).getTime() - Date.now()) / 1000;
  const abs = Math.abs(diff);
  if (abs < 45) return "just now";
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  if (abs < 86400 * 30) return rtf.format(Math.round(diff / 86400), "day");
  return dateFmt.format(new Date(iso));
}

export function formatDate(iso: string | undefined | null): string {
  if (!iso) return "";
  // Plain dates (YYYY-MM-DD) are calendar dates, not instants.
  if (/^\d{4}-\d{2}-\d{2}$/.test(iso)) {
    const [y, m, d] = iso.split("-").map(Number);
    return dateFmt.format(new Date(y, m - 1, d));
  }
  return dateFmt.format(new Date(iso));
}

export function formatDateTime(iso: string | undefined | null): string {
  return iso ? dateTimeFmt.format(new Date(iso)) : "";
}

/**
 * plural formats a count with the right form of its noun: "1 warning", "2 warnings", and with
 * the plural given, "1 person", "3 people".
 */
export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  return (parts[0][0] + (parts.length > 1 ? parts[parts.length - 1][0] : "")).toUpperCase();
}
