-- Replies are kept as plain agent messages; which message each answered and
-- how often a message was delivered are lost. A task whose agent is being
-- stopped to deliver a message at that moment is cancelled by that cleanup
-- instead of continued: the mark that says "continue" is dropped here.
ALTER TABLE agent_tasks DROP COLUMN IF EXISTS live_resume_generation;
DROP INDEX IF EXISTS agent_live_messages_replies;
ALTER TABLE agent_live_messages DROP CONSTRAINT IF EXISTS agent_live_messages_reply_check,
    DROP COLUMN IF EXISTS workflow_node,
    DROP COLUMN IF EXISTS deliveries,
    DROP COLUMN IF EXISTS reply_to;
