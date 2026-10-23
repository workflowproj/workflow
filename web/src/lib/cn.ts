import clsx, { type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** cn joins class names; later Tailwind classes win over conflicting earlier ones (e.g. w-40 over w-full). */
export function cn(...v: ClassValue[]) {
  return twMerge(clsx(v));
}
