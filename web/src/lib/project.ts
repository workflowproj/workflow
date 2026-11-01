/**
 * projectHome is the route a project opens on, its default view: a service project's queues, a
 * programme's roadmap, otherwise its board. Every link to "the project" (sidebar, project list,
 * after creating one, /projects/KEY) goes there.
 */
export function projectHome(p: { template: string }) {
  if (p.template === "service") return "/projects/$projectKey/queues" as const;
  if (isProgramme(p)) return "/projects/$projectKey/roadmap" as const;
  return "/projects/$projectKey/board" as const;
}

/** isProgramme reports a programme: initiatives that team projects' epics work toward. */
export function isProgramme(p: { template: string } | undefined) {
  return p?.template === "programme";
}
