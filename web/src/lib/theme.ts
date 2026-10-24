import { useSyncExternalStore } from "react";

/** The person's choice; "system" follows the operating system and changes with it. */
export type ThemePreference = "system" | "light" | "dark";
export type Theme = "light" | "dark";

const STORAGE_KEY = "wfm.theme"; // also read by public/theme-init.js before first paint

export function resolveTheme(pref: ThemePreference, systemDark: boolean): Theme {
  return pref === "dark" || (pref === "system" && systemDark) ? "dark" : "light";
}

function read(): ThemePreference {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system"; // storage can be unavailable (private windows); the choice just won't stick
  }
}

const media = typeof window !== "undefined" && window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
let preference: ThemePreference = typeof window !== "undefined" ? read() : "system";
const listeners = new Set<() => void>();

function apply() {
  document.documentElement.dataset.theme = resolveTheme(preference, !!media?.matches);
  listeners.forEach((l) => l());
}

if (typeof window !== "undefined") {
  media?.addEventListener("change", () => preference === "system" && apply());
  // Keep every open tab on the same theme.
  window.addEventListener("storage", (e) => {
    if (e.key === STORAGE_KEY) {
      preference = read();
      apply();
    }
  });
}

export function setThemePreference(pref: ThemePreference) {
  preference = pref;
  try {
    if (pref === "system") localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, pref);
  } catch {
    // ignore: see read()
  }
  apply();
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

/** useTheme returns the preference, the theme in effect, and a setter. */
export function useTheme(): { preference: ThemePreference; theme: Theme; setPreference: (p: ThemePreference) => void } {
  const pref = useSyncExternalStore(subscribe, () => preference, () => "system" as ThemePreference);
  const theme = useSyncExternalStore(subscribe, () => resolveTheme(preference, !!media?.matches), () => "light" as Theme);
  return { preference: pref, theme, setPreference: setThemePreference };
}
