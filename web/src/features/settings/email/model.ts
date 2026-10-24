import type { Schemas } from "@/api/client";

export type EmailDelivery = Schemas["EmailDelivery"];

export const deliveries: { value: EmailDelivery; label: string; hint: string }[] = [
  { value: "immediate", label: "Immediately", hint: "Within a couple of minutes; several changes to one issue arrive as one email." },
  { value: "digest", label: "Daily digest", hint: "One email a day at the time below, grouped by issue." },
  { value: "off", label: "Off", hint: "No email; the notification still appears in the app." },
];

/** formatHour renders a digest hour as a clock time, e.g. 9 → "09:00". */
export function formatHour(h: number): string {
  return `${String(h).padStart(2, "0")}:00`;
}

/** browserTimeZone is the zone the browser runs in (UTC when it cannot tell). */
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/** timeZoneOptions lists IANA zones for a picker, always including UTC and the current choice. */
export function timeZoneOptions(current: string, available: () => string[] = supportedZones): string[] {
  const zones = new Set(available());
  zones.add("UTC");
  if (current) zones.add(current);
  return [...zones].sort((a, b) => (a === "UTC" ? -1 : b === "UTC" ? 1 : a.localeCompare(b)));
}

function supportedZones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return [];
  }
}

/** nextDigestLabel describes when the next digest goes out, in the chosen zone. */
export function nextDigestLabel(iso: string, timeZone: string, locale?: string): string {
  try {
    // 24-hour, like the digest time picker.
    return new Intl.DateTimeFormat(locale, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone }).format(new Date(iso));
  } catch {
    return new Date(iso).toUTCString();
  }
}

type Settings = Schemas["EmailNotificationSettings"];
type Patch = Schemas["EmailNotificationSettingsPatch"];

/** preferencePatch is the request body that changes one kind's delivery. */
export function preferencePatch(kind: string, delivery: EmailDelivery): Patch {
  return { preferences: [{ kind, delivery }] };
}

/** mergePatch combines unsaved changes; later ones win, preferences by kind. */
export function mergePatch(a: Patch, b: Patch): Patch {
  const prefs = new Map([...(a.preferences ?? []), ...(b.preferences ?? [])].map((p) => [p.kind, p]));
  const out: Patch = { ...a, ...b };
  if (prefs.size) out.preferences = [...prefs.values()];
  return out;
}

/** applyPatch shows settings as they will be once unsaved changes are saved. */
export function applyPatch(s: Settings, p: Patch): Settings {
  const byKind = new Map((p.preferences ?? []).map((x) => [x.kind, x.delivery]));
  return {
    ...s,
    paused: p.paused ?? s.paused,
    digestHour: p.digestHour ?? s.digestHour,
    timeZone: p.timeZone ?? s.timeZone,
    preferences: s.preferences.map((x) => ({ ...x, delivery: byKind.get(x.kind) ?? x.delivery })),
  };
}
