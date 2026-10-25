# Sign-in, provisioning and access

## Sign-in (OpenID Connect)

People sign in with Google or any OpenID Connect provider. The authorisation code flow uses
PKCE, a nonce and a signed state cookie; sessions are server-side (revocable) and last
`WFM_SESSION_TTL` (7 days by default).

**Google** — create an OAuth client (type *Web application*) in Google Cloud Console with the
redirect URI `https://<WFM_PUBLIC_URL host>/auth/google/callback`, then:

```sh
WFM_GOOGLE_CLIENT_ID=…apps.googleusercontent.com
WFM_GOOGLE_CLIENT_SECRET=…
WFM_ALLOWED_DOMAINS=example.com          # strongly recommended
```

For Google the domain check uses the signed `hd` (hosted domain) claim, never the email suffix,
so personal Gmail accounts cannot sign in to a Workspace-restricted install.

**Other providers** (Okta, Entra ID, Keycloak, Authentik…) — list them by id:

```sh
WFM_OIDC_PROVIDERS=google,okta
WFM_OIDC_OKTA_ISSUER=https://example.okta.com
WFM_OIDC_OKTA_CLIENT_ID=…
WFM_OIDC_OKTA_CLIENT_SECRET=…
WFM_OIDC_OKTA_DISPLAY_NAME=Okta          # optional
WFM_OIDC_OKTA_ALLOWED_DOMAINS=example.com  # optional; overrides WFM_ALLOWED_DOMAINS
WFM_OIDC_OKTA_SCOPES=openid,email,profile  # optional
```

Redirect URI: `https://<host>/auth/<id>/callback`. For non-Google providers the allowed-domain
check uses the email's domain, and the provider must assert `email_verified`.

A person is matched to an existing account by provider subject, then (Google) Google id, then
verified email. Deactivated and deleted accounts cannot sign in.

## Who gets an account

Pick one source of truth:

| | When to use | Accounts are created | Deactivated when |
|---|---|---|---|
| **SCIM** | Okta, Entra ID, OneLogin, JumpCloud, Google (via a SCIM connector) | by the IdP when people are assigned to the app | unassigned / `active=false` / deleted in the IdP |
| **Google directory sync** | Google Workspace without a SCIM connector | every `WFM_GOOGLE_SYNC_INTERVAL` (15 min) for members of the access group or org unit | suspended, archived or removed from the group/OU; deleted |
| **Just-in-time** | small teams | on first sign-in | an admin deactivates them |

`WFM_JIT_PROVISIONING=auto` (default) enables just-in-time accounts only while no SCIM
connection exists and directory sync is off; set `true` or `false` to force it.
`WFM_BOOTSTRAP_ADMINS=alice@example.com,…` makes those people organisation admins whenever they
sign in or are provisioned. With none set, the very first account becomes the admin.

Deactivation is immediate: sessions are deleted and API tokens revoked. People deleted upstream
are deactivated, then anonymised after `WFM_DELETED_USER_RETENTION` (90 days) — their issues,
comments and history remain, attributed to "Former user". Re-provisioning someone within the
retention period restores the same account.

### SCIM 2.0

Administration → Sign-in & provisioning → *New SCIM connection* gives you the base URL
(`<WFM_HOOKS_PUBLIC_URL>/scim/v2`) and a bearer token (shown once; rotate any time). Create one
connection per IdP.

- Resources: `Users` and `Groups` with `GET` (filters such as `userName eq "…"`, pagination),
  `POST`, `PUT`, `PATCH` and `DELETE`; discovery endpoints `ServiceProviderConfig`,
  `ResourceTypes` and `Schemas`.
- User attributes: `userName`, `name.givenName`, `name.familyName`, `displayName`,
  `emails[primary]`, `externalId`, `active`. Other attributes are accepted and ignored.
- Okta: *SCIM 2.0 Test App (Header Auth)*, base URL as above, unique identifier `userName`,
  enable Push New Users, Push Profile Updates, Push Groups.
- Entra ID: Enterprise application → Provisioning → Automatic, Tenant URL = base URL, Secret
  Token = the token. Entra's PATCH quirks (`"active": "False"`, path-less replace, member
  removal by filter) are supported.
- SCIM-managed profiles are read-only in the UI; groups pushed by the IdP can only be mapped to
  an organisation role here.

### Google Workspace directory sync

1. In Google Cloud, create a service account and a JSON key; enable the Admin SDK API.
2. In the Admin console → Security → API controls → Domain-wide delegation, authorise the
   service account's client id for:
   `https://www.googleapis.com/auth/admin.directory.user.readonly`,
   `https://www.googleapis.com/auth/admin.directory.group.readonly`,
   `https://www.googleapis.com/auth/admin.directory.group.member.readonly`.
3. Configure:

```sh
WFM_GOOGLE_DIRECTORY_ENABLED=true
WFM_GOOGLE_SERVICE_ACCOUNT_KEY=/secrets/google/key.json   # path or the JSON itself
WFM_GOOGLE_ADMIN_SUBJECT=it-admin@example.com             # an admin to impersonate (read-only)
WFM_GOOGLE_SYNC_GROUP=workflow-users@example.com          # who gets access (and/or:)
WFM_GOOGLE_SYNC_ORG_UNIT=/Engineering
WFM_GOOGLE_SYNC_GROUPS=true                               # also mirror groups
WFM_GOOGLE_GROUP_PREFIX=team-                             # only groups whose email starts with this
```

With neither a group nor an org unit, every user in the domain gets an account. Mirrored groups
keep their membership in sync and can be used in project roles. Runs are listed under
Administration → Sign-in & provisioning, with a *Run now* button.

## Local accounts

Local accounts sign in with an email address and a password: contractors and others outside
your identity provider, or a small team without single sign-on. They are on by default
(`WFM_LOCAL_LOGIN=true`); with `false` the sign-in page shows no password form and every
password, reset and invitation endpoint answers 404.

**Adding someone.** Administration → Users → *Add person*, then choose how they sign in:

- *Email invitation* — a signed, single-use link valid for 7 days. *Resend invitation* replaces
  the link, *Revoke invitation* cancels it. Following the link, they choose a password and then
  sign in.
- *Temporary password* — generated and shown to you once; give it to them through a channel you
  trust. They must choose their own password at their first sign-in.
- *Single sign-on* (when an OIDC provider is configured) — no password; they are matched by email
  when they first sign in.

Local groups can be picked at the same time (`groupIds` on `POST /api/v1/admin/users`).

**Which people can have a password.** Anyone who is not managed by an identity provider:
people an admin added, people created just in time by single sign-on, and development
accounts. *Send invitation* or *Set temporary password* in a person's actions menu makes them a
local account; they then sign in either way, since single sign-on keeps matching their verified
email. *Remove password* makes them single sign-on only again (and signs them out). People
provisioned by SCIM or Google directory sync can never get a password, and when directory sync
takes over an existing local account (by matching its email) the account's password,
two-factor settings, recovery codes and open links are deleted.

**The first admin without single sign-on.** Run, with the server's configuration:

```sh
wfm create-admin --email you@example.com --name "Your Name"
```

It creates (or promotes) an organisation admin with a local account and prints a temporary
password. Running it again issues a new one — the recovery path if every admin is locked out.

**Passwords** are 12–128 characters and may not be one of the most common passwords (a compact
list from Have I Been Pwned's top 100,000 is built in), a common word padded with digits or
symbols (`Summer2024!!`, `P@ssw0rd123`), one character or a short pattern repeated, a run such as
`abcdefghijkl`, or contain the person's name, the local part of their email or their company's
domain name. Password fields show the strength while you type (the server's own check). Passwords
are hashed with argon2id (19 MiB, 2 iterations, 1 lane — the OWASP recommendation) and
re-hashed at the next sign-in if those parameters change.

- *Change password* (Profile → Sign-in and security) needs the current password, signs you out
  everywhere else and emails you.
- *Forgot password?* on the sign-in page emails a reset link valid for one hour. The answer is the
  same, and takes as long, whether or not the address has an account; at most three emails per
  address per 15 minutes. Resetting signs the person out everywhere and emails them; two-factor
  authentication still applies at the next sign-in. Admins can also *Send password reset*.

**Two-factor authentication** (TOTP, RFC 6238: SHA-1, 30-second steps, 6 digits, ±1 step for clock
drift, each code accepted once). Profile → Sign-in and security → *Set up*: scan the QR code (or
type the key) into an authenticator app, confirm with a code, and save the 10 one-time recovery
codes shown (stored hashed; *New recovery codes* replaces them). Once on, sign-in asks for a code
— or a recovery code — after the password. Turning it off needs the password and a code. Secrets
are encrypted with `WFM_SECRET_KEY`. With `WFM_LOCAL_REQUIRE_2FA=true`, every local account must
set it up at its next sign-in and cannot turn it off. Admins can *Reset two-factor* for someone who
lost their phone.

**Sign-in protection.**

- Throttling: 30 attempts a minute per client address across the sign-in endpoints, and a burst of
  10 (then 5 a minute) per email and address; beyond that the API answers 429 with `Retry-After`.
  Limits are kept in memory per replica. The client address comes from `X-Forwarded-For` only
  when the request arrives through a trusted proxy (`WFM_TRUSTED_PROXIES`), else it is the peer
  address.
- Lockout: `WFM_LOCAL_LOCKOUT_ATTEMPTS` (10) failed passwords or codes in a row — failures more
  than `WFM_LOCAL_LOCKOUT_DURATION` (15 minutes) apart start the count again — lock the account
  for `WFM_LOCAL_LOCKOUT_DURATION`, from any address (429, code `locked`). The lock is stored in
  the database; admins see it in the user list and can *Unlock*. Addresses without an account lock
  the same way, so a lockout reveals nothing.
- Errors are generic: a wrong email and a wrong password get the same 401, after the same argon2id
  work. Only someone who knows the right password learns that an account is deactivated.
- A finished sign-in is the same server-side session as single sign-on. Until then (a code, a
  password change or two-factor enrolment is still needed) the browser holds only a short-lived
  HttpOnly challenge cookie for `/api/v1/auth/local`, good for 5 attempts in 10 minutes.
- Personal access tokens cannot change passwords or two-factor settings.

**Audit log.** `user.login` (with `method: password`), `user.login_failed`, `user.locked`,
`user.password_changed`, `user.password_reset_requested`, `user.password_reset`,
`user.invitation_accepted`, `user.totp_enabled`, `user.totp_disabled`,
`user.recovery_code_used`, `user.recovery_codes_regenerated`, and the admin actions
`user.invitation_sent`, `user.invitation_revoked`, `user.temporary_password_set`,
`user.password_reset_sent`, `user.unlocked`, `user.totp_reset` and `user.password_removed`, each
with the client address.

**API.** Sign-in: `POST /api/v1/auth/local/login`, then `…/login/totp`, `…/login/new-password`,
`…/login/totp-setup` and `…/login/totp-setup/confirm` as the result's `status` asks;
`…/forgot-password`, `…/link` and `…/link/redeem` for emailed links; `…/password-check` for the
strength meter. Profile: `GET /api/v1/me/security`, `POST /api/v1/me/password`,
`/me/totp/setup`, `/me/totp/confirm`, `/me/totp/disable`, `/me/totp/recovery-codes`.
Administration: `/api/v1/admin/users/{id}/invitation` (POST, DELETE), `…/temporary-password`,
`…/password-reset`, `…/unlock`, `…/totp` (DELETE) and `…/password` (DELETE); the user list's
`localAccount` field reports each person's status.

## Roles and permissions

**Organisation roles**: `admin` (everything, including administration), `member` (sees open
projects and can create projects), `guest` (only projects they are explicitly added to). A group
can be mapped to an organisation role; a person gets the highest of their own and their
groups' roles. `customer` is for the people a [service desk](service-desk.md#customers)
supports: they only use the help centre, groups never raise their role, and they cannot be
project members.

**Projects** are *open* (every member gets the project's open role, `member` by default) or
*private*. Project roles — built-in `viewer`, `member`, `admin`, plus custom roles — are granted
to people or groups. Permissions:

| Permission | |
|---|---|
| `project.browse` | see the project and its issues (required by every role) |
| `issue.create`, `issue.edit`, `issue.edit_others`, `issue.delete` | |
| `issue.transition`, `issue.assign`, `issue.comment`, `comment.moderate` | |
| `sprint.manage`, `release.manage`, `board.manage` | planning |
| `project.admin` | settings, members, workflows, automation, repositories, webhooks |

Someone's permissions in a project are the union of the roles granted to them directly and via
groups. An explicit role replaces the open role, so adding someone as `viewer` to an open project
restricts them. Workflow conditions can check roles and permissions (`actor.hasRole("qa")`).

## API tokens and service accounts

Personal access tokens (`wfm_pat_…`, Profile → Tokens) act as their owner with a scope:
`read` (GET only), `write` (everything except administration) or `admin` (administration too;
org admins only). Tokens expire after 1–366 days and are stored hashed.

Service accounts (Administration → Users → *New service account*) are non-human users for CI and
scripts: give them project roles like people, and issue tokens for them. They cannot sign in
interactively.

Every security-relevant action — sign-ins, provisioning, role and membership changes, tokens,
workflow publishes, connections — is recorded in the audit log (Administration → Audit log,
`GET /api/v1/admin/audit`).
