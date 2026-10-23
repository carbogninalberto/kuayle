ALTER TABLE dev_machines
    DROP CONSTRAINT IF EXISTS dev_machines_machine_size_check;

ALTER TABLE dev_machines
    ADD CONSTRAINT dev_machines_machine_size_check
    CHECK (machine_size IN ('small', 'medium', 'large', 'custom'));
