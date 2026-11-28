-- One machine type on one image. Every Dev Machine is a terminal on the
-- single machine image beside its egress proxy (and optional Docker daemon):
-- the code-server IDE, the browser service, the activity collector and
-- Development Environments are removed.
--
-- Standard machines become headless. Their terminal shared the IDE container:
-- it keeps its record with no image, which the machine manager reads as
-- outdated, so the next start (or the idle image update of a running machine)
-- removes the IDE container and creates the terminal from the release's
-- machine image. The workspace volume is kept. Containers of the removed
-- services are removed then too (removeRetiredServiceContainers).

-- A standard machine created without the IDE had no terminal: give it one.
INSERT INTO dev_machine_services
    (workspace_id, machine_id, service_type, service_key, container_name, image_ref, internal_host, internal_port)
SELECT m.workspace_id, m.id, 'terminal', 'terminal', 'kuayle-' || m.routing_key || '-terminal', '',
    m.routing_key || '-terminal', 7681
FROM dev_machines m
WHERE m.profile = 'standard'
  AND NOT EXISTS (SELECT 1 FROM dev_machine_services s WHERE s.machine_id = m.id AND s.service_type = 'terminal');

UPDATE dev_machine_services s SET image_ref = ''
FROM dev_machines m
WHERE m.id = s.machine_id AND m.profile = 'standard' AND s.service_type = 'terminal';

-- Access sessions and tickets of the removed services go with them (cascade).
DELETE FROM dev_machine_services WHERE service_type IN ('ide', 'browser', 'collector');

ALTER TABLE dev_machine_services DROP CONSTRAINT dev_machine_services_service_type_check,
    ADD CONSTRAINT dev_machine_services_service_type_check CHECK (
        service_type IN ('terminal', 'agent', 'egress', 'docker'));

-- Variables for the developer container named it "ide" or "terminal": both
-- reach the one terminal now. The collector's token and the browser's
-- debugging token are no longer used.
DELETE FROM dev_machine_env_vars ide
WHERE ide.target_service = 'ide' AND EXISTS (
    SELECT 1 FROM dev_machine_env_vars terminal
    WHERE terminal.machine_id = ide.machine_id AND terminal.target_service = 'terminal' AND terminal.name = ide.name
      AND terminal.agent_run_id IS NOT DISTINCT FROM ide.agent_run_id
      AND terminal.provider_id IS NOT DISTINCT FROM ide.provider_id);
UPDATE dev_machine_env_vars SET target_service = 'terminal' WHERE target_service = 'ide';
DELETE FROM dev_machine_env_vars WHERE target_service IN ('browser', 'collector');

DROP TABLE dev_machine_tokens;

UPDATE dev_machines SET profile = 'headless' WHERE profile = 'standard';
ALTER TABLE dev_machines ALTER COLUMN profile SET DEFAULT 'headless',
    DROP CONSTRAINT dev_machines_profile_check,
    ADD CONSTRAINT dev_machines_profile_check CHECK (profile = 'headless');

-- Development Environments. Their snapshot images are removed from the host
-- by the machine manager when it starts (RemoveEnvironmentImages).
DELETE FROM dev_machine_operations WHERE action = 'snapshot_environment';
ALTER TABLE dev_machine_operations DROP COLUMN environment_id;
ALTER TABLE dev_machine_scope_settings DROP COLUMN environment_id;
ALTER TABLE dev_machines DROP COLUMN environment_id,
    DROP COLUMN environment_builder,
    DROP COLUMN services_config;
DROP TABLE dev_machine_environments;

-- The private-team guard checked for environments too.
CREATE OR REPLACE FUNCTION kuayle_private_external_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE wid UUID; BEGIN
    wid:=NEW.workspace_id;
    NULL; -- acquired before tuple locks by the statement trigger
    IF TG_TABLE_NAME='teams' THEN
        IF (TG_OP='INSERT' AND NEW.is_private) OR (TG_OP='UPDATE' AND NEW.is_private IS DISTINCT FROM OLD.is_private) THEN
            -- API callers acquire this BEFORE entering the exclusive graph barrier.
            -- A raw SQL writer must not invert that order and wait on publication.
            IF NOT pg_try_advisory_xact_lock(hashtextextended(wid::TEXT,63)) THEN
                RAISE EXCEPTION 'Publication in progress; retry visibility in a new transaction' USING ERRCODE='40001';
            END IF;
        END IF;
        IF NEW.is_private AND (TG_OP='INSERT' OR NOT OLD.is_private) AND (
            EXISTS(SELECT 1 FROM dev_machines WHERE workspace_id=wid)
            OR EXISTS(SELECT 1 FROM dev_machine_scope_settings WHERE workspace_id=wid)
        ) THEN
            RAISE EXCEPTION 'Remove development machines and scope settings before enabling private teams' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME IN ('dev_machines','dev_machine_scope_settings','dev_machine_workspace_policies') THEN
        IF EXISTS(SELECT 1 FROM workspace_privacy WHERE workspace_id=wid)
           AND (TG_TABLE_NAME<>'dev_machine_workspace_policies' OR (to_jsonb(NEW)->>'enabled')::BOOLEAN) THEN
            RAISE EXCEPTION 'Development machines are unavailable after private teams are enabled' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='github_auto_transitions' THEN
        IF NEW.target_status_id IS NOT NULL AND NOT EXISTS(
            SELECT 1 FROM team_statuses s JOIN teams t ON t.id=s.team_id
            WHERE s.id=NEW.target_status_id AND t.workspace_id=wid AND NOT t.is_private
        ) THEN
            RAISE EXCEPTION 'GitHub automation requires a public-team status' USING ERRCODE='23514';
        END IF;
    ELSIF NEW.issue_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM issues i JOIN teams t ON t.id=i.team_id
        WHERE i.id=NEW.issue_id AND i.workspace_id=wid AND NOT t.is_private
    ) THEN
        RAISE EXCEPTION 'GitHub activity requires a public issue' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
