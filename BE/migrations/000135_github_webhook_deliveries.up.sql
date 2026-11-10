-- One row per GitHub webhook delivery (X-GitHub-Delivery) the server accepted,
-- so a captured, validly signed delivery cannot be processed a second time.
-- installation_id is GitHub's installation ID from the signed payload (0 when
-- the event carries none). There is no foreign key on purpose: the marker must
-- outlive a disconnected installation, or a replayed "installation deleted"
-- delivery could disconnect the workspace again after it reconnects.
-- processed_at stays NULL while the delivery is being handled; such a claim
-- can be taken over after a short lease, so a crash does not lose the event.
CREATE TABLE github_webhook_deliveries (
    installation_id bigint NOT NULL,
    delivery_id text NOT NULL CHECK (char_length(delivery_id) BETWEEN 1 AND 128),
    claimed_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    PRIMARY KEY (installation_id, delivery_id)
);
CREATE INDEX github_webhook_deliveries_claimed_at ON github_webhook_deliveries(claimed_at);
