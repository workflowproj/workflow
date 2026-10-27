/**
 * projectHome is the route a project opens on, its default view: a service project's queues,
 * otherwise its board. Every link to "the project" (sidebar, project list, after creating one,
 * /projects/KEY) goes there.
 */
export function projectHome(p: { template: string }) {
  return p.template === "service" ? ("/projects/$projectKey/queues" as const) : ("/projects/$projectKey/board" as const);
}
