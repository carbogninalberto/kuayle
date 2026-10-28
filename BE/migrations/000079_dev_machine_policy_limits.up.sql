-- Task machines stay running and are reused across repositories, so the
-- machine limits default to 20 per user and 20 per workspace. Existing
-- workspace policies are raised to at least these limits; admins can lower
-- them again afterwards.
ALTER TABLE dev_machine_workspace_policies
    ALTER COLUMN max_concurrent_machines SET DEFAULT 20,
    ALTER COLUMN max_machines_per_user SET DEFAULT 20;

UPDATE dev_machine_workspace_policies
SET max_concurrent_machines = GREATEST(max_concurrent_machines, 20),
    max_machines_per_user = GREATEST(max_machines_per_user, 20),
    updated_at = NOW()
WHERE max_concurrent_machines < 20 OR max_machines_per_user < 20;
