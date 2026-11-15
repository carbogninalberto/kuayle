# Single sign-on with OpenID Connect

Kuayle supports OIDC single sign-on per workspace. It is free and ungated: there
is no licence key and no paid plan. SAML 2.0 and SCIM 2.0 are not implemented
yet; they are planned follow-ups.

An owner or admin configures the provider in **Workspace settings → SSO**. The
client secret is write-only: it is encrypted at rest, never returned by an API
response and never written to the logs. The page shows the exact redirect URI
to register at the identity provider.

## What a sign-in does

1. Kuayle sends the browser to the provider with an authorization-code request
   (PKCE `S256`, a random state and a random nonce). The flow state is stored
   server-side for ten minutes, keyed by the SHA-256 of the state, and bound to
   the browser with an HttpOnly cookie named `kuayle_sso_bind_<state digest>`
   whose path is that flow's callback. Two tabs or two workspaces can run at
   once without overwriting each other's binding.
2. The provider redirects to
   `https://<your-kuayle>/api/auth/sso/<workspace-slug>/callback`. Kuayle consumes
   the state once, checks the browser binding, exchanges the code with the PKCE
   verifier, and verifies the ID token signature, issuer, audience, expiry and
   nonce with the discovery document's JWKS.
3. Kuayle looks up the identity by `(workspace, issuer, subject)`.
   - A known identity signs that user in, but only while the user is still a
     member of the workspace and, when `allowed_domains` is set, only with a
     current email inside that list. Removing a member or narrowing the domains
     therefore cuts off returning identities too.
   - Otherwise the email must be present and `email_verified: true`. A missing
     claim counts as unverified.
   - The email domain must be in the workspace's allowed domains (an empty list
     allows any domain the provider verified; shared providers such as Google
     and Microsoft `/common` require a list, see below).
   - An existing Kuayle account with that verified email is **never linked
     automatically**: Kuayle refuses with `link_required`. The account owner
     signs in with their password (or passkey) and then links the identity from
     the profile page or the sign-in page. Only that authenticated session may
     bind the IdP subject. Being invited to, or added to, a workspace is not
     consent to link. An existing account that is **not yet a member** of the
     workspace must be invited or added first; linking itself never grants
     membership.
   - Otherwise Kuayle creates the account with a random unusable password and
     adds it to the workspace with the configured default role (`member` or
     `guest`). Just-in-time provisioning only covers emails that have no
     Kuayle account yet. Linking an existing account never adds membership.
4. The session carries the workspace that minted it, also across refresh-token
   rotation, and that scope is enforced on every request: an SSO session only
   works in the workspace whose IdP minted it.

Identities are unique per `(workspace, issuer, subject)`. A configuration in
workspace A therefore cannot authenticate into workspace B, and a state started
for A is refused on B's callback.

## The trust model of an SSO session

A workspace owner or admin controls that workspace's IdP and can mint any
subject, so an SSO-minted session is deliberately limited to its own workspace:

- Other workspaces return `403 SSO_SESSION_SCOPED`; the workspace list hides
  them.
- Account-level and durable actions are refused in the same way: registering or
  deleting passkeys, changing the profile or avatar, creating a personal access
  token or CLI link for any other workspace (or for all workspaces), creating
  or importing workspaces, accepting invites, and starting a system update.
- A personal access token or CLI connection link may be created only when it is
  restricted to exactly the SSO workspace's slug (`403 SSO_SESSION_SCOPED`
  otherwise). The token records that it was minted through this workspace's
  SSO, which is what lets it into the workspace when **Require SSO** is on (see
  below).

Use a password or passkey sign-in for account administration. This is what stops
an admin of one workspace from claiming a victim's email at their own IdP and
taking over the victim's other workspaces or credentials.

## Linking an existing account

An existing account is linked only by a session that already proved the
account's own credential, and only in a workspace it is already a member of:

1. If the account is not a member of the workspace yet, an owner or admin
   invites or adds it first. Signing in through the workspace's IdP does not
   create the membership for an existing account (`link_required`), and
   starting the link flow without membership is refused (`403 SSO_NOT_MEMBER`).
2. Sign in with the password (or passkey) as usual.
3. If the workspace offers SSO, the profile page shows **Link SSO sign-in**, or
   the `link_required` redirect on the sign-in page resumes the flow after the
   password sign-in.
4. The browser goes to the IdP, then back to the callback. Kuayle binds the
   subject to the signed-in account and signs it in with a scoped session. No
   workspace membership is added.

Members of a workspace that requires SSO who open it with a password session see
a notice with **Continue with SSO** that starts this link flow in place. An
existing account is never linked by a public SSO callback, even when the IdP
asserts a verified email and even when the account is already a member.


## Configure a provider

You need:

- **Issuer URL**: the OIDC discovery issuer. Kuayle fetches
  `<issuer>/.well-known/openid-configuration`. Most providers want the bare
  realm/tenant URL without `/.well-known/...`. The issuer is matched exactly as
  entered (only surrounding whitespace is trimmed): if the provider's discovery
  document ends with `/` (Auth0 does), keep the trailing slash.
- **Client ID** and **client secret**.
- **Redirect URI**: copy it from the settings page; it is
  `https://<your-kuayle>/api/auth/sso/<workspace-slug>/callback` (the exact
  origin is `FRONTEND_URL`).
- **PKCE**: authorization code flow with `S256`. Kuayle always sends a PKCE
  challenge; the provider must accept it (public or confidential client).
- **Scopes**: `openid email profile`.
- **Allowed email domains**: comma-separated, exact matches. Leave empty to
  accept any domain the provider verifies; the settings page warns when the
  list is empty. A **shared** provider signs in accounts of any organisation,
  so for `https://accounts.google.com` and
  `https://login.microsoftonline.com/{common,organizations,consumers}` (with or
  without `/v2.0`) at least one domain is required to save an enabled
  configuration. A configuration stored before this rule keeps working until it
  is next saved.
- **Default role**: `member` or `guest`.
- **Require SSO**: see below.

Saving validates the issuer URL and performs discovery. The issuer must use
HTTPS unless the instance sets `OIDC_ALLOW_PRIVATE_NETWORK=true` (the development
Compose profile does this for Keycloak). A validation error never echoes the
client secret.

### Provider notes

- **Keycloak**: create an OIDC client with *Client authentication* on, *Standard
  flow* on and *Require PKCE* set to `S256`. The issuer is
  `https://<keycloak>/realms/<realm>`.
- **Microsoft Entra ID**: use the tenant issuer
  `https://login.microsoftonline.com/<tenant-id>/v2.0` (the multi-tenant
  `/common`, `/organizations` and `/consumers` issuers need allowed
  domains), register a Web
  redirect URI, and enable ID tokens. `email` is a optional claim in some
  tenants; add it (and `email_verified` where available) or use the account's
  verified UPN.
- **Google**: issuer `https://accounts.google.com`, a Web application OAuth
  client, and the callback above. Google sends `email_verified`. Set the
  allowed domains to your Google Workspace domains: without them any Google
  account could join, so saving is refused.
- **Okta**: issuer `https://<org>.okta.com` (or the authorization server URL),
  a Web application with the callback above, and the `openid email profile`
  scopes.

## Require SSO and break-glass access

When **Require SSO** is on, a password or passkey session is refused for a
member or guest whose session was not minted by that workspace's SSO. The API
answers `403 SSO_REQUIRED` with the workspace slug; the UI points the person at
the SSO sign-in entry.

Owners keep password sign-in for that workspace as the break-glass path. An
owner should keep an up-to-date password (and ideally a passkey) so a
misconfigured identity provider cannot lock the workspace out.

Password and passkey sign-in are refused only when **every** workspace the
account belongs to requires SSO, the account has a linked identity in each of
them and the account owns none of them. An account without a linked identity can
still sign in with its password and link: never lock out the very people who
need to link. Members of several workspaces keep password access to the ones that
do not require SSO.

### Personal access tokens and CLI links

A workspace that requires SSO accepts only personal access tokens (including
tokens minted by a CLI connection link) that were created from a session
signed in through **that workspace's SSO**. Anything else gets
`403 SSO_REQUIRED` with the workspace slug, both when the credential is created
and on every API request:

- From a password or passkey session, `POST /api/tokens` and
  `POST /api/cli-links` refuse a `workspace_slugs` list that names a workspace
  requiring SSO.
- An empty or missing `workspace_slugs` list means *all workspaces*. It is
  refused when the account belongs to any workspace that requires SSO; restrict
  the token to the other workspaces instead.
- A CLI link is checked again when it is redeemed, so a link created before
  **Require SSO** was turned on mints nothing for that workspace.
- **Turning Require SSO on stops older tokens** for that workspace, unless they
  were minted through its SSO. Other workspaces the token is valid for are
  unaffected. Recreate the token after signing in with SSO (choose this
  workspace only).
- Owners get **no** break-glass for tokens: the owner exemption covers the
  interactive password sign-in only. An owner who needs API access to the
  workspace signs in with SSO and creates a token from that session.

Revoking a departing user's tokens when the IdP deactivates them needs SCIM 2.0
deprovisioning, which is a later phase; until then, revoke their tokens manually
or remove them from the workspace (a token never outlives the membership).

## Instance-level configuration

These variables pin one workspace's OIDC configuration to the instance. They
win over the workspace settings, and the settings page becomes read-only:

```env
SSO_OIDC_WORKSPACE_SLUG=acme
SSO_OIDC_ISSUER=https://idp.example.com/realms/acme
SSO_OIDC_CLIENT_ID=kuayle
SSO_OIDC_CLIENT_SECRET=<generated-secret>
SSO_OIDC_ALLOWED_DOMAINS=example.com,example.org
SSO_OIDC_DEFAULT_ROLE=member
SSO_OIDC_REQUIRE_SSO=false
OIDC_ALLOW_PRIVATE_NETWORK=false
```

`SSO_OIDC_WORKSPACE_SLUG`, `SSO_OIDC_ISSUER` and `SSO_OIDC_CLIENT_ID` are
required together. The issuer must use HTTPS unless
`OIDC_ALLOW_PRIVATE_NETWORK=true`. `SSO_OIDC_CLIENT_SECRET` is rejected when it
is an example placeholder outside `ENVIRONMENT=development`.
`SSO_OIDC_ALLOWED_DOMAINS` is required when the issuer is a shared provider
(Google, Microsoft `/common`, `/organizations` or `/consumers`); startup fails
otherwise.

If `SSO_OIDC_WORKSPACE_SLUG` names a workspace that does not exist, the
instance configuration does not apply: every workspace keeps its own settings
and the server logs a warning (with the slug only) at startup and on the first
lookup. A workspace created later with that slug picks up the instance
configuration within 30 seconds, without a restart. The workspace ID is cached
once found, so deleting and recreating the workspace with the same slug needs a
restart.

## Local development with Keycloak

The Compose file ships a Keycloak profile with an imported realm, the `kuayle`
client (PKCE `S256`, secret `kuayle-dev-client-secret`) and a verified test user
`demo` / `demo`.

1. Start the identity provider:

   ```sh
   docker compose --profile sso up -d keycloak
   ```

   The admin console is at <http://keycloak.localhost:8081> (`admin` / `admin`).

2. Run Kuayle from source with the API on port 8080 and the UI on 5173 (or run
   the Compose `app` profile). The UI proxies `/api` to the backend.

3. Sign in as a workspace owner or admin, open **Settings → SSO**, and enter:

   | Field | Value |
   | --- | --- |
   | Provider name | Keycloak |
   | Issuer URL | `http://keycloak.localhost:8081/realms/kuayle` |
   | Client ID | `kuayle` |
   | Client secret | `kuayle-dev-client-secret` |
   | Allowed email domains | `example.com` |
   | Default role | Member |
   | Enabled | on |

   The redirect URI shown on the page is covered by the realm wildcard
   (`http://localhost:5173/api/auth/sso/*`, wildcard at the end).

4. Sign out and use **Sign in with SSO** with the workspace slug. Sign in as
   `demo` / `demo`. The first sign-in creates the user `demo@example.com` and
   adds it to the workspace as a member.

5. Turn on **Require SSO** and confirm that a member's password sign-in is
   rejected with `SSO_REQUIRED` while the owner's password sign-in still works.

If the backend runs in the Compose `app` profile, the `extra_hosts` entry maps
`keycloak.localhost` to the host so discovery and token exchange use the same
issuer the browser uses. `OIDC_ALLOW_PRIVATE_NETWORK` defaults to `true` in the
development Compose file.

## Troubleshooting

- **`SSO_NOT_CONFIGURED`**: the workspace has no enabled configuration, or the
  slug does not exist.
- **`invalid_state`**: the flow expired (ten minutes), was already used, was
  started in another browser, or belongs to another workspace. Start again.
- **`email_unverified`**: the provider did not assert `email_verified: true`.
  Configure the provider to send a verified email claim.
- **`domain_not_allowed`**: the verified email's domain is not in the allowed
  list.
- **`token_invalid`**: discovery, the token exchange, PKCE, the signature,
  issuer, audience, expiry or nonce check failed. Check the issuer URL, client
  ID/secret and the provider's clock.
- **`link_required`**: an account with this verified email already exists.
  Sign in with the password (or passkey) and use **Link SSO sign-in**. If the
  account is not a member of this workspace yet, an owner or admin must invite
  or add it first; linking never grants membership.
- **`not_member`**: the identity is linked, but the account is no longer a
  member of the workspace, or it tried to link while not a member.
- **`identity_taken`**: the IdP subject is already linked to another account in
  this workspace. Use a different IdP subject or unlink the other account.
- **`SSO_SESSION_SCOPED`**: the request came from an SSO session and targets
  another workspace or a durable/account-level action. Sign in with the password
  or passkey for those actions.
- **`provider_error`**: the provider redirected back with an error. Its text is
  deliberately not shown or logged; check the provider's own logs.

## Not implemented yet

- **SAML 2.0**: SP metadata, IdP metadata by URL or upload, signed assertion and
  audience validation. It can reuse `user_identities` with `provider='saml'`.
- **SCIM 2.0**: a bearer token per workspace, user/group provisioning, and
  deactivation that deletes the user's refresh tokens and revokes their PATs in
  one transaction, so a user deactivated at the IdP loses API access at once.
