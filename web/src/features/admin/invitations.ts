/**
 * What to tell whoever invited someone, or sent them a password reset, when the server could
 * not email it (no SMTP server: the API answers emailed: false). The link exists, nobody
 * received it, and these say what to do instead.
 */

export const NO_EMAIL = "this server can't send email yet (no SMTP server is set up)";

/** invitationSent is the outcome of an admin's invitation to a person. */
export function invitationSent(name: string, email: string, emailed: boolean): string {
  return emailed
    ? `Invitation sent to ${email}`
    : `Invitation for ${name} not emailed: ${NO_EMAIL}. Set a temporary password instead, or set up email (Administration → Email) and resend the invitation.`;
}

/** resetSent is the outcome of an admin's password reset for a person. */
export function resetSent(name: string, email: string, emailed: boolean): string {
  return emailed
    ? `Password reset link sent to ${email}`
    : `Password reset for ${name} not emailed: ${NO_EMAIL}. Set a temporary password instead, or set up email (Administration → Email).`;
}

/** customerAdded is the outcome of adding someone to a help desk's customers. */
export function customerAdded(added: { customer: { user: { displayName: string; email: string } }; invited: boolean; emailed: boolean }): string {
  const { displayName, email } = added.customer.user;
  if (!added.invited) return `${displayName} can now raise requests`;
  if (added.emailed) return `Invitation sent to ${email}`;
  return `Added ${displayName}, but their invitation was not emailed: ${NO_EMAIL}. Ask an administrator to set up email, or to give them a temporary password (Administration → Users).`;
}
