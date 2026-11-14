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
   the browser with an HttpOnly `kuayle_sso_bind` cookie.
2. The provider redirects to
   `https://<your-kuayle>/api/auth/sso/<workspace-slug>/callback`. Kuayle consumes
   the state once, checks the browser binding, exchanges the code with the PKCE
   verifier, and verifies the ID token signature, issuer, audience, expiry and
   nonce with the discovery document's JWKS.
3. Kuayle looks up the identity by `(workspace, issuer, subject)`.
   - A known identity signs that user in.
   - Otherwise the email must be present and `email_verified: true`. A missing
     claim counts as unverified.
   - The email domain must be in the workspace's allowed domains (an empty list
     allows any domain the provider verified).
   - An existing Kuayle account with that verified email is linked. The account
     is never linked on an unverified email.
   - Otherwise Kuayle creates the account with a random unusable password and
     adds it to the workspace with the configured default role (`member` or
     `guest`).
4. The session carries the workspace that minted it, also across refresh-token
   rotation, so require-SSO enforcement does not disappear when the access token
   is refreshed.

Identities are unique per `(workspace, issuer, subject)`. A configuration in
workspace A therefore cannot authenticate into workspace B, and a state started
for A is refused on B's callback.

## Configure a provider

You need:

- **Issuer URL**: the OIDC discovery issuer. Kuayle fetches
  `<issuer>/.well-known/openid-configuration`. Most providers want the bare
  realm/tenant URL without `/.well-known/...`.
- **Client ID** and **client secret**.
- **Redirect URI**: copy it from the settings page; it is
  `https://<your-kuayle>/api/auth/sso/<workspace-slug>/callback` (the exact
  origin is `FRONTEND_URL`).
- **PKCE**: authorization code flow with `S256`. Kuayle always sends a PKCE
  challenge; the provider must accept it (public or confidential client).
- **Scopes**: `openid email profile`.
- **Allowed email domains**: optional, comma-separated, exact matches. Leave
  empty to accept any domain the provider verifies.
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
  `https://login.microsoftonline.com/<tenant-id>/v2.0`, register a Web
  redirect URI, and enable ID tokens. `email` is a optional claim in some
  tenants; add it (and `email_verified` where available) or use the account's
  verified UPN.
- **Google**: issuer `https://accounts.google.com`, a Web application OAuth
  client, and the callback above. Google sends `email_verified`.
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

Password and passkey sign-in are also refused with `403 SSO_REQUIRED` only when
**every** workspace the account belongs to requires SSO and the account owns
none of them. Members of several workspaces keep password access to the ones
that do not require SSO.

Personal access tokens are **not** affected in phase 1: a PAT remains usable in a
workspace that requires SSO. SCIM 2.0 deprovisioning (phase 3) will revoke a
user's refresh tokens and PATs together; until then, revoke a departing user's
tokens manually from **Settings → API tokens**.

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
- **`provider_error`**: the provider redirected back with an error. Its text is
  deliberately not shown or logged; check the provider's own logs.

## Not implemented yet

- **SAML 2.0**: SP metadata, IdP metadata by URL or upload, signed assertion and
  audience validation. It can reuse `user_identities` with `provider='saml'`.
- **SCIM 2.0**: a bearer token per workspace, user/group provisioning, and
  deactivation that deletes the user's refresh tokens and revokes their PATs in
  one transaction. That also closes the phase-1 PAT caveat above.
