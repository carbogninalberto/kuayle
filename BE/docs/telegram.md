# Telegram conversation channel

Telegram uses the same private `conversations`, `conversation_messages` and ordered
`conversation_events` records as the Global Assistant. It is an additional channel,
not a separate orchestrator. It supports general assistant chat (including the
assistant's issue tools), new conversations, cancellation, completed replies and
[approval buttons](#approval-buttons) and [task notifications](#task-notifications).

## Setup

Open **Settings → Telegram**. The workspace owner can save or replace a BotFather
token, enable the bot, edit up to 100 numeric Telegram **user** IDs, or remove the
token. A blank token field preserves an existing token; Remove explicitly deletes
it. An empty allowlist permits nobody. Usernames, negative group IDs, and IDs
outside Telegram's 52-bit range are rejected. Read APIs return configuration state
and bot username, never the token or webhook secret. Non-owners see only their
own account-link state, not the allowlist or credential controls.

Use a dedicated bot for each workspace: one bot ID can be configured only once
per Kuayle database. Enabling verifies the token with `getMe` and installs a secret
header authenticated webhook at `FRONTEND_URL/api/telegram/webhook/:workspaceId`.
`FRONTEND_URL` must be the publicly reachable HTTPS application origin; the reverse
proxy must forward `/api` and the Telegram secret header to the API. Local HTTP
development cannot receive a real Telegram webhook directly; set
`TELEGRAM_WEBHOOK_URL` to an HTTPS relay channel (for example a dedicated smee.io
channel, not the GitHub one) and forward it to
`http://localhost:8080/api/telegram/webhook/<workspaceId>`, e.g.
`npx smee-client --url <channel> --target <that URL>`. A relay shows every update,
including the secret header, to anyone with the channel URL, so use it only with a
test bot; a later save on another installation rotates the secret and moves the
webhook. Save again after fixing an
unreachable origin. A failed webhook installation leaves saved configuration with
`connection_failed`, rather than claiming a live connection. `connected` means
Telegram accepted registration, not a continuous reachability health check.

Each allowed user opens the same settings page, enters their own numeric Telegram
ID, and generates a one-time `/start` command. Send it privately to the configured
bot within ten minutes. The random code is stored only as a hash, tied to the
workspace, Kuayle user and expected Telegram ID. Another allowed Telegram account
cannot redeem it. New codes replace older pending codes for that Kuayle user. Successful linking
invalidates all pending codes for the Telegram ID; an existing binding to
a different Kuayle user cannot be taken over. Users can unlink themselves.

The bot accepts private messages only, requires `message.from.id == chat.id`,
rejects bot senders and ignores unsupported update types. Every message rechecks
the current allowlist, binding, and non-guest workspace membership. It never grants
the workspace owner's permissions to other Telegram users. Removing an ID deletes
its binding and pending link code; adding it again requires a new account link.

Commands:

- `/new`: start a new conversation, keeping the old conversation in Web history.
- `/stop`: cancel the current reply, using the same fenced state as Web Stop.
- Other text: send to the configured workspace assistant. If a reply is already
  active, the bot asks the user to wait or stop before resending; it does not
  silently discard a queued user turn or run concurrent generations.

Replies from either Web or Telegram in the currently linked conversation are
queued in the same transaction as the assistant message. Telegram receives a
bounded plain-text reply and a link to the full Web conversation. Link previews
are disabled. Web clients continue seeing progressive events; Telegram receives
the completed reply. Messages typed on the Web are not echoed individually back
to Telegram; the corresponding assistant reply is delivered.

## Security and persistence

Migration 54 adds settings, hashed link codes, identity bindings, update receipts
and an outgoing delivery queue. Bot tokens use the existing authenticated encryption
helper with a separate key derived from `JWT_SECRET`. Token rotation changes the
webhook authenticator. Changing to a different bot deletes identity bindings and
outgoing deliveries. Removing configuration preserves Web conversation history.
Update receipts are retained across configuration removal to avoid replay when the
same bot is added again. No raw Telegram update payload or link command is stored.

Webhook identity comes from Telegram's secret header, compared in constant time,
not from query strings. A bounded preflight checks it without locking settings
before parsing input or loading AI configuration. The existing public IP rate
limiter protects the webhook. Bodies are bounded. Update receipts and conversation
writes commit together, preventing duplicate generations on Telegram retries.
Browser settings and account-link routes are session-only; PATs are rejected.
Owner checks also run in the service and repository. Settings writes serialize
remote registration across API replicas with a PostgreSQL advisory transaction
lock. Telegram calls target the fixed official API, refuse redirects, use public
address filtering and a ten-second timeout; errors never include token-bearing
request URLs or remote response bodies.

The outgoing worker claims a delivery durably before external I/O and rechecks
current settings, membership and binding before sending. For an issue-scoped
conversation it also checks the original/current team boundary and pins the issue
and team membership until delivery completes. Revoked or moved issue chats cannot
accept Telegram input or deliver queued output. It holds the relevant
configuration/membership locks during the bounded HTTP call so a completed
revocation cannot be followed by a newly authorized send. Telegram `sendMessage`
has no application idempotency key: ambiguous HTTP failures and crash-interrupted
sends are **not retried automatically**. They remain `uncertain` (stale `sending`
claims become uncertain after one minute); the complete reply remains available
in the Web conversation. This intentionally favors no duplicate sends over a
promise of exactly-once external delivery.

While a reply is written, the same checks gate a streaming preview (migration
`000078_telegram_streaming`): the chat shows "typing…" until text exists, then one
message is sent and edited with the text so far at most every two seconds. The
final delivery edits that message (falling back to a new message) and adds any
approval buttons. Replies are sent as Telegram HTML converted from the assistant's
Markdown (bold, italic, strikethrough, code, fenced blocks, quotes, lists and
http(s) links; everything else escaped), and as plain text if Telegram refuses the
formatting. The link back to the conversation is clickable only when
`FRONTEND_URL` is HTTPS; Telegram does not link `localhost`.

Disabling blocks input and queued output immediately; authenticated updates are
acknowledged and ignored while disabled. Re-enabling or switching bots uses
`drop_pending_updates` to discard Telegram’s backlog from before activation.
Removing a token also removes
the local webhook authority. It does not call `deleteWebhook` with a removed token;
Telegram can retry that old endpoint until the bot is reconfigured or its webhook
is removed through Telegram. These requests cannot access Kuayle conversations.
Rolling back migration 54 deletes Telegram configuration, links and delivery state,
but preserves conversations and issues. Do not run an older schema with a newer API.

## Task notifications

When a coding task becomes ready for review, completed, failed or blocked, its
creator receives a Telegram notice if they have linked Telegram on an enabled
bot. The notice is queued in the same transaction as the task transition. It
names the issue, the new state and the recorded reason, plus the pull request
link once its publication was recorded, and links to the issue conversation. The
delivery worker applies the usual allowlist, membership, binding and issue-scope
checks at send time, so a moved or revoked issue chat is not delivered. Other
transitions (queued, preparing, running, validating, publishing) do not notify.

## Approval buttons

When a delivered reply contains pending approvals bound to its recipient (for
example a `delegate_issue` request), the message carries one inline row per
approval (at most three): **Approve ENG-1** and **Deny**. Buttons are attached
inside the delivery transaction after the usual allowlist, membership, binding
and issue-scope checks. Callback data only names the approval
(`approve:<uuid>`/`deny:<uuid>`); it grants nothing by itself.

A button press (`callback_query`) is accepted only from a private chat whose ID
equals the sender, from a non-bot, allowlisted, linked Telegram user with
non-guest membership, with a valid secret header. Forged data and group presses
are ignored before any lookup. The update receipt deduplicates Telegram retries.
The decision then runs as the linked Kuayle user through the same
`ResolveApproval` path as the Web UI, so the bound actor, expiry, final denial,
single claim and idempotent task creation all apply; approvals of other users
look missing. The bot answers the press (`answerCallbackQuery`) and removes the
buttons once the decision is final (`editMessageReplyMarkup`); both calls are best
effort because the decision is already durable. A Web decision made first is
reported as already decided.

## Verification

Real PostgreSQL tests cover owner-only settings, unauthorized IDs, an empty list,
wrong link identity, competing link codes, concurrent duplicate updates, invalid
webhook secrets, membership loss, concurrent Web/Telegram writes, idle recovery,
and revocation after a delivery was claimed. Service integration uses a local
Telegram API fixture to verify encrypted persistence, safe read responses,
getMe/setWebhook, one-time linking, shared Web history, Web-originated replies,
callback rejection, and outgoing plain-text replies. Transport tests cover redirect
refusal, safe errors and UTF-16 reply bounds. HTTP route tests cover JWT/guest/PAT
access. Migration tests verify rollback preserves conversations and issues.
Playwright covers owner token/allowlist updates/removal, invalid ID input, member
linking, and mobile layout. No live bot token or real Telegram account is used.

An approval test drives a scripted assistant reply in a Telegram conversation and
checks the delivered keyboard, presses from an unlinked user, forged data and a
group chat (ignored), approval with exactly one task, a redelivered update, a later
denial, and the Web UI seeing the same decision.

Protocol reference: [Telegram Bot API](https://core.telegram.org/bots/api), including
`setWebhook.secret_token`, `Update`, `User`, `sendMessage`, `CallbackQuery`,
`answerCallbackQuery` and `editMessageReplyMarkup`.
