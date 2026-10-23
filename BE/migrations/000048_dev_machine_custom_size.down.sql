ALTER TABLE dev_machines
    DROP CONSTRAINT IF EXISTS dev_machines_machine_size_check;

UPDATE dev_machines SET machine_size='large' WHERE machine_size='custom';

ALTER TABLE dev_machines
    ADD CONSTRAINT dev_machines_machine_size_check
    CHECK (machine_size IN ('small', 'medium', 'large'));
