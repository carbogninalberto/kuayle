-- Which agent acted on the user's behalf: a coding harness or the assistant.
ALTER TABLE issue_history ADD COLUMN agent text CHECK (agent IS NULL OR agent ~ '^[a-z-]{1,64}$');

-- Content history records the acting agent from a transaction-local setting,
-- like the actor, so it never leaks across pooled connections.
CREATE OR REPLACE FUNCTION kuayle_record_issue_content_history() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE actor UUID := NULLIF(current_setting('kuayle.history_actor', true), '')::UUID;
        agent TEXT := NULLIF(current_setting('kuayle.history_agent', true), '');
        changed_at TIMESTAMPTZ := clock_timestamp();
BEGIN
    IF OLD.title IS DISTINCT FROM NEW.title THEN
        INSERT INTO issue_history(issue_id, user_id, field, old_value, new_value, created_at, agent)
        VALUES (NEW.id, actor, 'title', OLD.title, NEW.title, changed_at, agent);
    END IF;
    IF COALESCE(OLD.description, '') IS DISTINCT FROM COALESCE(NEW.description, '') THEN
        INSERT INTO issue_history(issue_id, user_id, field, old_value, new_value, created_at, agent)
        VALUES (NEW.id, actor, 'description', OLD.description, NEW.description, changed_at, agent);
    END IF;
    RETURN NEW;
END;
$$;
