import { describe, expect, it } from "vitest";
import { orgRoleChoices, roleChangeQuestion, withArticle } from "./org-roles";

describe("organisation roles", () => {
  it("offers customers their own role, and never makes service accounts admins or customers", () => {
    expect(orgRoleChoices({ kind: "human", orgRole: "customer" })).toEqual(["admin", "member", "guest", "customer"]);
    expect(orgRoleChoices({ kind: "human", orgRole: "member" })).toContain("customer");
    expect(orgRoleChoices({ kind: "service", orgRole: "member" })).toEqual(["member", "guest"]);
    // A role held anyway is still shown as it is, not as another one.
    expect(orgRoleChoices({ kind: "service", orgRole: "customer" })).toEqual(["member", "guest", "customer"]);
  });

  it("asks before turning a customer into an agent, an agent into a customer, or anyone into an admin", () => {
    const toMember = roleChangeQuestion("Carol", "customer", "member");
    expect(toMember?.title).toBe("Make Carol a member?");
    expect(toMember?.body).toContain("help desk customer");
    expect(toMember?.confirmLabel).toBe("Make member");
    expect(roleChangeQuestion("Carol", "customer", "admin")?.title).toBe("Make Carol an admin?");
    const toCustomer = roleChangeQuestion("Dan", "member", "customer");
    expect(toCustomer?.danger).toBe(true);
    expect(toCustomer?.body).toContain("loses access");
    expect(roleChangeQuestion("Dan", "member", "admin")?.confirmLabel).toBe("Make admin");
    expect(roleChangeQuestion("Dan", "member", "guest")).toBeNull();
    expect(roleChangeQuestion("Dan", "guest", "member")).toBeNull();
  });

  it("names roles in sentences", () => {
    expect(withArticle("admin")).toBe("an admin");
    expect(withArticle("customer")).toBe("a customer");
  });
});
