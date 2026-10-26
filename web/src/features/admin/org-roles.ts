import type { Schemas } from "@/api/client";

type OrgRole = Schemas["OrgRole"];

export const ORG_ROLE_LABEL: Record<OrgRole, string> = { admin: "Admin", member: "Member", guest: "Guest", customer: "Customer" };

/** withArticle names a role in a sentence: "an admin", "a customer". */
export function withArticle(role: OrgRole): string {
  return `${role === "admin" ? "an" : "a"} ${role}`;
}

/**
 * orgRoleChoices are the organisation roles someone can be given: service accounts are never
 * admins or customers. A role they have anyway (set before these rules) stays listed, so the
 * select shows the truth.
 */
export function orgRoleChoices(user: { kind: Schemas["UserKind"]; orgRole: OrgRole }): OrgRole[] {
  const roles: OrgRole[] = user.kind === "service" ? ["member", "guest"] : ["admin", "member", "guest", "customer"];
  return roles.includes(user.orgRole) ? roles : [...roles, user.orgRole];
}

const agentAccess: Record<Exclude<OrgRole, "customer">, string> = {
  admin: "Admins can change every project and the organisation's settings.",
  member: "Members see every open project and can create projects.",
  guest: "Guests see only the projects they are added to.",
};

export type RoleChangeQuestion = { title: string; body: string; confirmLabel: string; danger?: boolean };

/**
 * roleChangeQuestion is what to ask before changing someone's organisation role, or null when
 * the change needs no confirmation. Making someone an admin, turning a help desk customer into
 * an agent and an agent into a customer are each deliberate acts.
 */
export function roleChangeQuestion(name: string, from: OrgRole, to: OrgRole): RoleChangeQuestion | null {
  if (from === to) return null;
  if (to === "customer") {
    return {
      title: `Make ${name} a customer?`,
      body: `Customers only use the help centre. ${name} loses access to projects, boards and issues: their project memberships stop counting and their tokens stop working.`,
      confirmLabel: "Make customer",
      danger: true,
    };
  }
  if (from === "customer") {
    return {
      title: `Make ${name} ${withArticle(to)}?`,
      body: `${name} is a help desk customer, who only uses the help centre. ${agentAccess[to]} They can still raise requests in the help centre.`,
      confirmLabel: `Make ${to}`,
    };
  }
  if (to === "admin") return { title: `Make ${name} an admin?`, body: agentAccess.admin, confirmLabel: "Make admin" };
  return null;
}
