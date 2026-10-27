/**
 * showDevelopment says whether an issue's Development section has anything to offer. Software
 * projects always show it (with how to connect a repository); a service project's requests show
 * it only once a repository is connected or something is linked, which a help desk rarely has.
 * Until the repositories are known (undefined), a service request shows nothing rather than
 * flashing the section.
 */
export function showDevelopment({ service, repositories, linked }: { service: boolean; repositories: number | undefined; linked: number }): boolean {
  return !service || linked > 0 || (repositories ?? 0) > 0;
}
