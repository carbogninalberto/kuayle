DROP TABLE task_verdicts;
ALTER TABLE task_messages DROP CONSTRAINT task_messages_author_member;
DROP TABLE task_members;
ALTER TABLE agent_profiles DROP CONSTRAINT agent_profiles_id_workspace;
ALTER TABLE agent_tasks DROP CONSTRAINT agent_tasks_id_workspace;
