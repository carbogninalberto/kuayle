DELETE FROM conversation_attachments WHERE live_message_id IS NOT NULL;
ALTER TABLE conversation_attachments DROP COLUMN live_message_id;
