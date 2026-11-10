# Passkeys (WebAuthn)

A passkey is a WebAuthn credential a user registers on a device or security key and
then uses to sign in without a password. Kuayle is the relying party; ceremonies are
verified with [go-webauthn](https://github.com/go-webauthn/webauthn).

Code: [service](../internal/service/passkey.go), [relying party validation](../internal/service/passkey_rp.go),
[handler](../internal/handler/passkey.go), [repository](../internal/repository/passkey.go),
[migration](../migrations/000138_passkeys.up.sql), [session check](../internal/middleware/auth.go).
Tests: [service](../internal/service/passkey_test.go) (no database),
[HTTP + PostgreSQL](../cmd/server/passkey_http_test.go), with the software authenticator in
[internal/passkeytest](../internal/passkeytest/authenticator.go).

## Decisions

- **Passkeys are added to an account that has a password; there are no passwordless-only
  accounts.** Every sign-up path (`POST /api/auth/register`, with or without an invite)
  sets a password, there is no endpoint that removes one, and passkey sign-in never
  creates an account. A user can therefore remove all of their passkeys and still sign in.
- **Should an account ever have no usable password, its last passkey cannot be removed.**
  "Usable" means the stored hash is a bcrypt hash. This cannot happen through the product
  today; the rule exists so a hash blanked by an operator, or a future passwordless
  sign-up, cannot lock an account out. The removal answers `409 LAST_PASSKEY`. Such an
  account also cannot add a passkey from an old session by password; it signs in again
  with a passkey it has, which makes the session fresh.
- **A password reset removes every passkey of the account**, together with its sessions
  and personal access tokens. Reset is the recovery path for a taken-over account, and
  whoever took it over may have added a passkey. The reset page tells the user so.
- **User verification is required** (biometric or PIN) at registration and sign-in. A
  passkey replaces the password, so presence alone is not enough.
- **Passkeys are discoverable credentials** (`residentKey: required`). Sign-in names no
  account; the authenticator offers what it holds for this relying party.
- **No attestation policy.** `attestation: "none"` is requested. Any AAGUID is accepted,
  including unknown ones and all zeros; `none` and self (`packed` without a certificate
  chain) attestation are accepted. A statement that is present is still verified. The
  AAGUID is stored and returned for display only.
- **Passkeys are off in public demo mode** (`PUBLIC_DEMO_MODE=true`): demo accounts are
  shared and a visitor's passkey would be a private way into one.

## Relying party configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `PASSKEYS_ENABLED` | `true` | `false` turns passkey ceremonies off. |
| `PASSKEY_RP_ID` | host of the first origin | The domain passkeys are bound to. Each origin's host must equal it or be a subdomain of it. |
| `PASSKEY_RP_ORIGINS` | `FRONTEND_URL` | Comma-separated browser origins (`scheme://host[:port]`) allowed to use passkeys. |
| `PASSKEY_RP_DISPLAY_NAME` | `Kuayle` | Name shown by the browser and authenticator. |

A default installation needs none of these: the relying party is `FRONTEND_URL`.

Rules, checked at start-up by `NewPasskeyRelyingParty`:

- An origin must be `https`, or `http` on `localhost` / `*.localhost`. **Plain http anywhere
  else disables passkeys**: browsers only expose WebAuthn in a secure context. The server
  logs `passkeys are disabled: ...` with the reason and keeps running with password
  sign-in; it does not refuse to start.
- The relying party ID is a bare host name. IP addresses and public suffixes (`co.uk`)
  are refused.
- Changing the relying party ID orphans every registered passkey: a passkey is bound to
  the ID it was created for. Choosing a parent domain (`example.com` for
  `kuayle.example.com`) keeps passkeys valid if the host name changes later, at the cost
  of letting every other site under that domain request them.

When ceremonies are disabled they answer `403 PASSKEYS_DISABLED`, `GET /api/config`
reports `"passkeys_enabled": false` and the UI hides the options. Stored passkeys can
still be listed, renamed and removed.

### Behind a reverse proxy

The accepted origin is **only** the configured list. It is never derived from `Host`,
`X-Forwarded-Host`, `X-Forwarded-Proto` or `Forwarded`, so a proxy that rewrites the host,
or a client that forges those headers, changes nothing. What is verified is the origin
the browser put inside the signed `clientDataJSON`.

The usual failure is an instance reached through an address other than `FRONTEND_URL`
(an internal host name, a second domain). To make that visible instead of failing inside
the browser's prompt, `begin` endpoints compare the request's `Origin` header with the
list and answer `400 PASSKEY_ORIGIN_MISMATCH`, logging the received and allowed origins.
Fix it by setting `FRONTEND_URL` to the public origin or listing every public origin in
`PASSKEY_RP_ORIGINS` under a common `PASSKEY_RP_ID`.

## Endpoints

All under `/api/auth/passkey`, rate limited like the other auth routes where noted.
Bodies and responses are JSON. Errors use the standard `{"error": {"code", "message"}}`.

| Method and path | Caller | Purpose |
| --- | --- | --- |
| `POST /login/begin` | anonymous, rate limited | Start a sign-in. Returns `{"publicKey": PublicKeyCredentialRequestOptions}`. |
| `POST /login/complete` | anonymous, rate limited | Body `{"credential": <assertion JSON>}`. Sets the session cookies and returns the user, exactly like `POST /api/auth/login`. |
| `POST /register/begin` | session, rate limited | Body `{}` or `{"password": "..."}`. Returns `{"publicKey": PublicKeyCredentialCreationOptions}`. |
| `POST /register/complete` | session, rate limited | Body `{"name": "...", "credential": <attestation JSON>}`. Returns the passkey, `201`. |
| `GET /credentials` | session | The caller's passkeys. |
| `PATCH /credentials/:id` | session | Body `{"name": "..."}` (1 to 64 characters). |
| `DELETE /credentials/:id` | session | Remove a passkey and the sessions it minted. |

"Session" means an interactive session: personal access tokens are refused
(`sessionOnly`), so a token can neither add a way to sign in nor inspect the existing
ones. Another user's passkey ID answers `404`.

The credential JSON is what `PublicKeyCredential.toJSON()` produces (base64url fields).
A passkey is returned as:

```json
{
  "id": "…uuid…", "name": "Work laptop", "transports": ["internal"],
  "aaguid": "…uuid…", "backed_up": false,
  "created_at": "…", "last_used_at": null, "current": false
}
```

`current` marks the passkey the calling session signed in with. Public keys and
credential IDs are never returned.

| Code | Status | When |
| --- | --- | --- |
| `PASSKEYS_DISABLED` | 403 | No usable relying party on this instance. |
| `PASSKEY_ORIGIN_MISMATCH` | 400 | Request or response origin is not an allowed origin. |
| `REAUTH_REQUIRED` | 403 | `register/begin` on a session that is not fresh, without a password. |
| `INVALID_PASSWORD` | 403 | `register/begin` with a wrong password. |
| `ACCOUNT_LOCKED` | 429 | Too many wrong passwords on `register/begin` (5 in 15 minutes). |
| `PASSKEY_CHALLENGE_EXPIRED` | 400 | No pending ceremony for this browser: never started, expired, already completed, replaced, or another user's. |
| `PASSKEY_INVALID` | 401 sign-in, 400 registration | The response failed verification, including a signature counter that did not advance. |
| `PASSKEY_UNKNOWN` | 401 | The credential is not registered (never was, or was removed). |
| `PASSKEY_ALREADY_REGISTERED` | 409 | The credential ID is already stored. |
| `PASSKEY_LIMIT` | 409 | The account already has 20 passkeys. |
| `LAST_PASSKEY` | 409 | See Decisions. |

## Sessions

Passkey sign-in ends in `AuthService.issueSession`, the same function password sign-in,
registration and refresh rotation use, and the handler sets cookies through
`AuthHandler.setAuthCookies`. There is one session mechanism.

Two facts are recorded per session, in the refresh-token row and as access-token claims,
and carried over unchanged by refresh rotation:

- `authenticated_at` / `auth_time`: when the user last proved who they are.
- `passkey_credential_id` / `pkc`: the passkey that minted the session, if any.

**Removing a passkey ends the sessions it minted, and only those.** The refresh tokens
go with the passkey through `ON DELETE CASCADE`. Access tokens are stateless and live up
to 15 minutes, so for a token carrying `pkc` the authentication middleware checks on every
request that the passkey still belongs to the user (one primary-key lookup; password
sessions are not affected). Sessions by password or by another passkey continue.

**Adding a passkey needs a fresh session or the password.** Fresh means `auth_time` within
the last 10 minutes. Otherwise `register/begin` answers `REAUTH_REQUIRED` until the
request carries the account password. Sessions that predate this feature have no
`auth_time` and count as not fresh.

## Challenges

`begin` stores the ceremony's state in `passkey_challenges` and gives the browser an
opaque random handle in an `HttpOnly`, `SameSite=Strict` cookie scoped to
`/api/auth/passkey` (`__Host-kuayle-passkey` at `/` with host-prefixed cookies). Only the
SHA-256 of the handle is stored; the WebAuthn challenge itself never leaves the server
except inside the options.

- **Single use.** `complete` deletes the row (`DELETE … RETURNING`) before verifying
  anything. A second request with the same handle, concurrent or later, finds nothing,
  whatever the first one's outcome. A failed attempt burns its challenge too.
- **Bound.** The handle cookie binds the ceremony to the browser that started it. A
  registration is additionally bound to the user: it completes only for the user who
  began it. A login challenge cannot complete a registration and vice versa.
- **Expiring.** Five minutes, enforced by `expires_at` and by the library's own deadline.
- **Renewed by the sign-in page.** The page starts a ceremony on load so the browser can
  offer passkeys in the email field, and replaces it every four minutes while it stays
  open, so the offer never outlives its challenge.
- **Not kept across attempts.** A new registration deletes the user's previous one; a new
  login deletes the one the same browser held. Expired rows are purged on every `begin`.

## Threat model

| Threat | Defence |
| --- | --- |
| Phishing site relays the ceremony | The browser signs its own origin; only configured origins are accepted and the relying party ID hash must match. |
| Replay of a captured assertion or registration response | The challenge is single-use and consumed before verification; the signature covers the challenge. |
| Response completed from another browser or by another user | The handle cookie is required; registrations are bound to their user. |
| Stolen session (access token) adds the thief's passkey | Registration needs a fresh sign-in or the password; password guesses through that endpoint are throttled per account. Within 10 minutes of sign-in a stolen session can still add one: the window is the cost of not asking for the password after every sign-in. |
| Personal access token adds or reads passkeys | Passkey management is session-only. |
| Cloned authenticator | A signature counter that does not advance is rejected, the stored counter is left unchanged, and `auth.passkey_clone_suspected` is logged with the user, passkey and both counters. Passkeys synced between devices report 0 forever; that is accepted and gives no clone detection, by design of such passkeys. |
| Unknown or removed credential | Rejected with `PASSKEY_UNKNOWN` and logged (`auth.passkey_login_failed`, `reason=unknown_credential`). A user handle naming a different account than the credential's owner is rejected the same way. |
| Passkey removed while a sign-in with it is in flight | The sign-in fails: the counter update and the session row both require the passkey to exist. |
| Removed passkey's session keeps working | Refresh tokens cascade; access tokens are checked against the passkey on each request. |
| Forged `Host` / `X-Forwarded-*` | Not used for any decision. |
| Account takeover followed by a password reset | The reset removes all passkeys, sessions and personal access tokens. |
| Authenticator without user verification | Rejected at registration and at sign-in. |

Out of scope: attestation-based authenticator allow-lists and FIDO metadata checks,
account recovery other than password reset, and telling the user's browser to forget a
removed passkey (WebAuthn signal API).

Authenticators that return no user handle are supported: the credential ID identifies
the passkey and its owner. Platform authenticators and roaming security keys are treated
alike; a roaming key must support discoverable credentials and user verification.

## Audit log events

`auth.passkey_registered`, `auth.passkey_deleted`, `auth.login` with `method=passkey`,
`auth.passkey_login_failed` and `auth.passkey_register_failed` with a `reason`
(`origin_mismatch`, `challenge_missing_or_expired`, `unknown_credential`,
`verification_failed`) and a `detail`, `auth.passkey_clone_suspected` (error level),
`auth.passkey_reauth_failed`. Clients are never sent the detail.
