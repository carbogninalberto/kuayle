-- Retention prunes expired usage samples of every machine, stopped ones
-- included, by age alone; the per-machine index cannot serve that scan.
CREATE INDEX IF NOT EXISTS idx_dev_machine_resource_samples_created_at
    ON dev_machine_resource_samples(created_at);
