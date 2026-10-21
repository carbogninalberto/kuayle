-- No backfill: earlier versions can only be reviewed when they were recorded.
ALTER TABLE issue_history ALTER COLUMN user_id DROP NOT NULL;

CREATE FUNCTION kuayle_record_issue_content_history() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE actor UUID := NULLIF(current_setting('kuayle.history_actor', true), '')::UUID;
        changed_at TIMESTAMPTZ := clock_timestamp();
BEGIN
    IF OLD.title IS DISTINCT FROM NEW.title THEN
        INSERT INTO issue_history(issue_id, user_id, field, old_value, new_value, created_at)
        VALUES (NEW.id, actor, 'title', OLD.title, NEW.title, changed_at);
    END IF;
    IF COALESCE(OLD.description, '') IS DISTINCT FROM COALESCE(NEW.description, '') THEN
        INSERT INTO issue_history(issue_id, user_id, field, old_value, new_value, created_at)
        VALUES (NEW.id, actor, 'description', OLD.description, NEW.description, changed_at);
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER issue_content_history AFTER UPDATE OF title, description ON issues
FOR EACH ROW EXECUTE FUNCTION kuayle_record_issue_content_history();
