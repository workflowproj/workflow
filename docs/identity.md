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

## Roles and permissions

**Organisation roles**: `admin` (everything, including administration), `member` (sees open
projects and can create projects), `guest` (only projects they are explicitly added to). A group
can be mapped to an organisation role; a person gets the highest of their own and their
groups' roles.

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
