# Instance Web Push settings

Migration 189 adds `instance_settings`, keyed by name. Its repository serializes
updates to each key in a transaction. Values marked secret use the existing
AES-GCM helper with a key derived from `JWT_SECRET` for instance settings.
Web Push stores its key pair, subject and enable flag together under `web_push`.
Rolling back migration 189 deletes the stored settings, including the pair.

The validated VAPID environment configuration takes precedence. Otherwise the
public-key endpoint, subscription registration, delivery worker and test sends
read the app configuration from the database. The public enrollment endpoint
returns an empty key while disabled. Keep the same JWT secret on all replicas and
when restoring a backup. See the [operator guide](../../docs/self-hosting.md#web-push-optional).

All four routes below require an interactive, unscoped session for a user listed
in `SYSADMINS`. Status and mutation responses include only the public key.

| Method | Path | Contract |
| --- | --- | --- |
| GET | `/api/system/push/settings` | Returns `source` (off, app, environment), `public_key`, `subject`, `enabled`, `read_only` and the caller’s `subscribed_devices` count. |
| POST | `/api/system/push/keys` | Generates a pair. An existing pair requires `{"confirm":true}`. The response sets `requires_reenrollment` on rotation. |
| PUT | `/api/system/push/settings` | Saves `subject` and `enabled`. Enabling requires a pair and a mailto or HTTPS contact. Disabling preserves the keys. |
| POST | `/api/system/push/test` | Sends a generic notification to the caller’s own subscriptions and returns provider acceptance counts (`sent`, `failed`). Requires enabled delivery. |

Both mutation routes return 409 when the environment controls the configuration.
An unconfirmed rotation also returns 409. Test delivery stays available with
environment configuration and uses the normal provider timeout, retry and
expired-subscription cleanup. The personal event preferences apply to ordinary
notifications. Operators can explicitly test delivery independently of those preferences.
