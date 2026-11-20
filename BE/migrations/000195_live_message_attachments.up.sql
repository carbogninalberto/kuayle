ALTER TABLE conversation_attachments
    ADD COLUMN live_message_id uuid REFERENCES agent_live_messages(id) ON DELETE CASCADE,
    ADD CHECK (message_id IS NULL OR live_message_id IS NULL);
CREATE INDEX idx_conversation_attachments_live_message
    ON conversation_attachments(live_message_id, position) WHERE live_message_id IS NOT NULL;
