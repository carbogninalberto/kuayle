-- The individual SET NULL foreign-key actions run before all related references
-- have necessarily been cleared. Keep the audit checks intact by detaching the
-- dependent references together before the parent cascade starts.
CREATE FUNCTION detach_machine_access_logs_before_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE dev_machine_access_logs
    SET machine_id = NULL, service_id = NULL
    WHERE workspace_id = OLD.workspace_id AND machine_id = OLD.id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER detach_machine_access_logs_before_delete
BEFORE DELETE ON dev_machines
FOR EACH ROW EXECUTE FUNCTION detach_machine_access_logs_before_delete();

CREATE FUNCTION detach_workspace_machine_access_logs_before_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE dev_machine_access_logs
    SET workspace_id = NULL, machine_id = NULL, service_id = NULL
    WHERE workspace_id = OLD.id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER detach_workspace_machine_access_logs_before_delete
BEFORE DELETE ON workspaces
FOR EACH ROW EXECUTE FUNCTION detach_workspace_machine_access_logs_before_delete();
