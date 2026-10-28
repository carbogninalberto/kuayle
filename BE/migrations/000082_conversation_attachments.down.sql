-- Rolling back deletes conversation image records and task attachment lists.
-- Stored objects are not removed; issue assets copied from them are kept.
ALTER TABLE ai_settings DROP COLUMN IF EXISTS assistant_vision;
DROP TABLE IF EXISTS agent_task_attachments;
DROP TRIGGER IF EXISTS conversation_attachment_garbage ON conversation_attachments;
DROP FUNCTION IF EXISTS kuayle_conversation_attachment_garbage();
DROP TABLE IF EXISTS conversation_attachment_garbage;
DROP TABLE IF EXISTS conversation_attachment_issues;
DROP TABLE IF EXISTS conversation_attachments;
