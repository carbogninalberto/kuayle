-- Restores the schema of migration 214. The data the up migration removed is
-- not restored: machines stay headless (terminal only), the IDE, browser and
-- collector service records and their variables are gone, machines have no
-- collector token (the previous release's collector cannot report events),
-- and no Development Environment exists. Recreate machines that need the IDE.

CREATE TABLE dev_machine_environments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    image_ref TEXT NOT NULL,
    image_digest VARCHAR(255),
    status VARCHAR(32) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'building', 'ready', 'failed', 'delete_requested')),
    source_machine_id UUID,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    delete_requested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, name),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, source_machine_id, id)
);
ALTER TABLE dev_machine_environments
    ADD CONSTRAINT fk_dev_machine_environments_source_machine FOREIGN KEY (workspace_id, source_machine_id)
        REFERENCES dev_machines(workspace_id, id) ON DELETE SET NULL (source_machine_id);

CREATE INDEX idx_dev_machine_environments_delete_requested
    ON dev_machine_environments(delete_requested_at, updated_at)
    WHERE status = 'delete_requested';
CREATE TRIGGER private_external_guard BEFORE INSERT OR UPDATE ON dev_machine_environments
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_external_guard();
CREATE TRIGGER privacy_statement_shared BEFORE INSERT OR UPDATE OR DELETE ON dev_machine_environments
    FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('shared');
CREATE TRIGGER dev_machine_environments_touch BEFORE UPDATE ON dev_machine_environments
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
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
            OR EXISTS(SELECT 1 FROM dev_machine_environments WHERE workspace_id=wid)
            OR EXISTS(SELECT 1 FROM dev_machine_scope_settings WHERE workspace_id=wid)
        ) THEN
            RAISE EXCEPTION 'Remove development machines, environments and scope settings before enabling private teams' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME IN ('dev_machines','dev_machine_environments','dev_machine_scope_settings','dev_machine_workspace_policies') THEN
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

ALTER TABLE dev_machines ADD COLUMN environment_id UUID,
    ADD COLUMN environment_builder BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN services_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT fk_dev_machines_environment FOREIGN KEY (workspace_id, environment_id)
        REFERENCES dev_machine_environments(workspace_id, id) ON DELETE SET NULL (environment_id);
ALTER TABLE dev_machine_scope_settings ADD COLUMN environment_id UUID,
    ADD CONSTRAINT fk_dev_machine_scope_settings_environment FOREIGN KEY (workspace_id, environment_id)
        REFERENCES dev_machine_environments(workspace_id, id) ON DELETE SET NULL (environment_id);
ALTER TABLE dev_machine_operations ADD COLUMN environment_id UUID,
    ADD CONSTRAINT fk_dev_machine_operations_environment FOREIGN KEY (workspace_id, machine_id, environment_id)
        REFERENCES dev_machine_environments(workspace_id, source_machine_id, id) ON DELETE CASCADE;

ALTER TABLE dev_machines ALTER COLUMN profile SET DEFAULT 'standard',
    DROP CONSTRAINT dev_machines_profile_check,
    ADD CONSTRAINT dev_machines_profile_check CHECK (profile IN ('standard', 'headless'));

CREATE TABLE dev_machine_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    scopes JSONB NOT NULL,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_dev_machine_tokens_agent_run FOREIGN KEY (machine_id, agent_run_id)
        REFERENCES dev_machine_agent_runs(machine_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_dev_machine_tokens_active ON dev_machine_tokens(token_hash, expires_at)
    WHERE revoked_at IS NULL;
CREATE TRIGGER privacy_statement_shared BEFORE INSERT OR UPDATE OR DELETE ON dev_machine_tokens
    FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('shared');

ALTER TABLE dev_machine_services DROP CONSTRAINT dev_machine_services_service_type_check,
    ADD CONSTRAINT dev_machine_services_service_type_check CHECK (
        service_type IN ('ide', 'terminal', 'agent', 'browser', 'collector', 'egress', 'docker'));
