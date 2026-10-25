ALTER TABLE dev_machine_workspace_policies
    ALTER COLUMN allowed_providers SET DEFAULT
        '["claude-code","opencode","codex","pi","qwen-code","crush","kimi","goose"]'::jsonb;

ALTER TABLE dev_machines
    ADD COLUMN profile VARCHAR(16) NOT NULL DEFAULT 'standard';

ALTER TABLE dev_machines
    ADD CONSTRAINT dev_machines_profile_check
    CHECK (profile IN ('standard','headless'));
