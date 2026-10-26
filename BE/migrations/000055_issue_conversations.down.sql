-- Scoped history cannot safely survive as a general conversation. Only issue
-- conversations and their cascading messages/events/deliveries are removed.
DELETE FROM conversations WHERE issue_id IS NOT NULL;
ALTER TABLE conversations DROP COLUMN issue_id, DROP COLUMN team_id;
ALTER TABLE issues DROP CONSTRAINT issues_conversation_scope;
