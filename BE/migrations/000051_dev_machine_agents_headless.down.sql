ALTER TABLE dev_machines
    DROP CONSTRAINT IF EXISTS dev_machines_profile_check;

ALTER TABLE dev_machines
    DROP COLUMN IF EXISTS profile;

ALTER TABLE dev_machine_workspace_policies
    ALTER COLUMN allowed_providers SET DEFAULT
        '["claude-code","opencode","codex"]'::jsonb;
