-- Telegram replies stream: while the assistant writes, one Telegram message is
-- sent and then edited with the text so far; the final delivery replaces it.
ALTER TABLE telegram_deliveries
 ADD COLUMN telegram_message_id bigint,
 ADD COLUMN preview_text text NOT NULL DEFAULT '' CHECK (octet_length(preview_text) <= 32768),
 ADD COLUMN preview_at timestamptz;
