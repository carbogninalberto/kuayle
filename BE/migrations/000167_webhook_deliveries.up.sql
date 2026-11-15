-- Durable outgoing webhook deliveries (KUA-197). One row per change and
-- subscribed webhook, stored when the change is emitted. Workers in any
-- instance send due rows and retry failures with backoff; a row is pending
-- exactly while it has a next attempt. body holds the exact bytes that are
-- signed and sent, and is cleared once the delivery is finished: the log keeps
-- only metadata, and finished rows are purged after a few days.
CREATE TABLE webhook_deliveries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seq              bigint GENERATED ALWAYS AS IDENTITY,
    webhook_id       uuid NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    workspace_id     uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    event            text NOT NULL,
    body             text NOT NULL,
    status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts         integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at  timestamptz,
    last_status_code integer,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    completed_at     timestamptz,
    CHECK ((status = 'pending') = (next_attempt_at IS NOT NULL)),
    CHECK ((status = 'pending') = (completed_at IS NULL))
);
CREATE INDEX webhook_deliveries_pending ON webhook_deliveries(workspace_id, seq) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_log ON webhook_deliveries(webhook_id, seq DESC);
CREATE INDEX webhook_deliveries_completed ON webhook_deliveries(completed_at) WHERE status <> 'pending';

-- One row per workspace that has had deliveries. A worker locks it while it
-- sends one of the workspace's deliveries, so each workspace has one delivery
-- in flight across all instances, and last_turn_at lets workspaces take turns.
CREATE TABLE webhook_lanes (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    last_turn_at timestamptz
);

-- Circuit breaker per webhook, shared by all instances: consecutive failed
-- deliveries, and the time until which the webhook is paused.
CREATE TABLE webhook_circuits (
    webhook_id           uuid PRIMARY KEY REFERENCES webhooks(id) ON DELETE CASCADE,
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    paused_until         timestamptz
);
