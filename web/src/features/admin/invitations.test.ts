import { describe, expect, it } from "vitest";
import { customerAdded, invitationSent, resetSent } from "./invitations";

describe("invitation messages", () => {
  it("say an invitation was sent only when it was emailed", () => {
    expect(invitationSent("Lola", "lola@example.com", true)).toBe("Invitation sent to lola@example.com");
    const kept = invitationSent("Lola", "lola@example.com", false);
    expect(kept).toContain("not emailed");
    expect(kept).not.toContain("sent to");
    expect(kept).toContain("temporary password");
    expect(resetSent("Lola", "lola@example.com", true)).toBe("Password reset link sent to lola@example.com");
    expect(resetSent("Lola", "lola@example.com", false)).toContain("not emailed");
  });

  it("tell agents whether a new customer's invitation reached them", () => {
    const user = { displayName: "Carol", email: "carol@customer.test" };
    expect(customerAdded({ customer: { user }, invited: true, emailed: true })).toBe("Invitation sent to carol@customer.test");
    expect(customerAdded({ customer: { user }, invited: false, emailed: false })).toBe("Carol can now raise requests");
    const kept = customerAdded({ customer: { user }, invited: true, emailed: false });
    expect(kept).toMatch(/^Added Carol, but their invitation was not emailed/);
    expect(kept).toContain("administrator");
  });
});
