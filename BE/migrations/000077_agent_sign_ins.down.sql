-- The enum value stays: PostgreSQL cannot drop enum values.
ALTER TABLE agent_tasks DROP COLUMN sign_in_required;
DROP TABLE agent_sign_ins;
